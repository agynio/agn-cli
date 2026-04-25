package tokenizer

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func (c *counter) countPDFTokens(data []byte) (int, error) {
	text, dims, err := extractPDFText(data)
	if err != nil {
		return 0, err
	}

	count := c.countText(text)
	for _, dim := range dims {
		count += pageImageTokens(dim)
	}
	return count, nil
}

func extractPDFText(data []byte) (string, []types.Dim, error) {
	reader := bytes.NewReader(data)
	ctx, err := api.ReadAndValidate(reader, model.NewDefaultConfiguration())
	if err != nil {
		return "", nil, fmt.Errorf("parse pdf: %w", err)
	}

	dims, err := ctx.PageDims()
	if err != nil {
		return "", nil, fmt.Errorf("read pdf page dimensions: %w", err)
	}
	if len(dims) != ctx.PageCount {
		return "", nil, errors.New("pdf page dimensions are incomplete")
	}

	var builder strings.Builder
	for page := 1; page <= ctx.PageCount; page++ {
		contentReader, err := pdfcpu.ExtractPageContent(ctx, page)
		if err != nil {
			return "", nil, fmt.Errorf("read page content: %w", err)
		}
		if contentReader == nil {
			continue
		}
		contentBytes, err := io.ReadAll(contentReader)
		if err != nil {
			return "", nil, fmt.Errorf("read page content: %w", err)
		}
		pageText, err := extractTextFromContent(contentBytes)
		if err != nil {
			return "", nil, fmt.Errorf("extract page text: %w", err)
		}
		pageText = normalizeText(pageText)
		if pageText == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString(" ")
		}
		builder.WriteString(pageText)
	}

	return builder.String(), dims, nil
}

func pageImageTokens(dim types.Dim) int {
	if dim.Width <= 0 || dim.Height <= 0 {
		panic("invalid pdf page dimensions")
	}
	widthPx := dim.Width * 150.0 / 72.0
	heightPx := dim.Height * 150.0 / 72.0
	return gpt5ImageTokens(widthPx, heightPx)
}

type tokenType int

const (
	tokenOperator tokenType = iota
	tokenString
	tokenArray
	tokenOther
)

type token struct {
	typeName tokenType
	value    string
	values   []string
}

type operandKind int

const (
	operandString operandKind = iota
	operandArray
)

type operand struct {
	kind   operandKind
	value  string
	values []string
}

type contentScanner struct {
	data []byte
	pos  int
}

func extractTextFromContent(content []byte) (string, error) {
	scanner := contentScanner{data: content}
	operands := make([]operand, 0, 4)
	var builder strings.Builder
	var hasText bool

	for {
		ok, err := scanner.skipWhitespace()
		if err != nil {
			return "", err
		}
		if !ok {
			break
		}
		tok, err := scanner.nextToken()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", err
		}
		switch tok.typeName {
		case tokenString:
			operands = append(operands, operand{kind: operandString, value: tok.value})
		case tokenArray:
			operands = append(operands, operand{kind: operandArray, values: tok.values})
		case tokenOperator:
			if tok.value == "BI" {
				scanner.skipInlineImage()
				operands = operands[:0]
				continue
			}
			text := normalizeText(extractTextOperand(tok.value, operands))
			if text != "" {
				if hasText {
					builder.WriteString(" ")
				}
				builder.WriteString(text)
				hasText = true
			}
			operands = operands[:0]
		default:
			// ignore
		}
	}

	return builder.String(), nil
}

func normalizeText(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
}

func extractTextOperand(op string, operands []operand) string {
	switch op {
	case "Tj", "'", "\"":
		if value, ok := lastStringOperand(operands); ok {
			return value
		}
	case "TJ":
		if values, ok := lastArrayOperand(operands); ok {
			return strings.Join(values, "")
		}
	}
	return ""
}

func lastStringOperand(operands []operand) (string, bool) {
	for i := len(operands) - 1; i >= 0; i-- {
		if operands[i].kind == operandString {
			return operands[i].value, true
		}
	}
	return "", false
}

func lastArrayOperand(operands []operand) ([]string, bool) {
	for i := len(operands) - 1; i >= 0; i-- {
		if operands[i].kind == operandArray {
			return operands[i].values, true
		}
	}
	return nil, false
}

func (s *contentScanner) skipWhitespace() (bool, error) {
	for s.pos < len(s.data) {
		ch := s.data[s.pos]
		if isWhitespace(ch) {
			s.pos++
			continue
		}
		if ch == '%' {
			for s.pos < len(s.data) && s.data[s.pos] != '\n' && s.data[s.pos] != '\r' {
				s.pos++
			}
			continue
		}
		return true, nil
	}
	return false, nil
}

func (s *contentScanner) nextToken() (token, error) {
	if s.pos >= len(s.data) {
		return token{}, io.EOF
	}
	ch := s.data[s.pos]
	switch ch {
	case '(':
		value, err := s.parseLiteralString()
		if err != nil {
			return token{}, err
		}
		return token{typeName: tokenString, value: value}, nil
	case '<':
		if s.pos+1 < len(s.data) && s.data[s.pos+1] == '<' {
			s.skipDictionary()
			return token{typeName: tokenOther}, nil
		}
		value, err := s.parseHexString()
		if err != nil {
			return token{}, err
		}
		return token{typeName: tokenString, value: value}, nil
	case '[':
		values, err := s.parseArray()
		if err != nil {
			return token{}, err
		}
		return token{typeName: tokenArray, values: values}, nil
	case '/':
		s.pos++
		_ = s.readToken()
		return token{typeName: tokenOther}, nil
	case '"':
		s.pos++
		return token{typeName: tokenOperator, value: "\""}, nil
	case '\'':
		s.pos++
		return token{typeName: tokenOperator, value: "'"}, nil
	default:
		value := s.readToken()
		if value == "" {
			s.pos++
			return token{typeName: tokenOther}, nil
		}
		return token{typeName: tokenOperator, value: value}, nil
	}
}

func (s *contentScanner) readToken() string {
	start := s.pos
	for s.pos < len(s.data) {
		ch := s.data[s.pos]
		if isDelimiter(ch) || isWhitespace(ch) || ch == '%' {
			break
		}
		s.pos++
	}
	return string(s.data[start:s.pos])
}

func (s *contentScanner) parseLiteralString() (string, error) {
	if s.data[s.pos] != '(' {
		return "", errors.New("expected string literal")
	}
	s.pos++
	var buf bytes.Buffer
	depth := 1
	for s.pos < len(s.data) {
		ch := s.data[s.pos]
		s.pos++
		if ch == '\\' {
			if s.pos >= len(s.data) {
				return "", errors.New("unterminated string escape")
			}
			next := s.data[s.pos]
			s.pos++
			switch next {
			case 'n':
				buf.WriteByte('\n')
			case 'r':
				buf.WriteByte('\r')
			case 't':
				buf.WriteByte('\t')
			case 'b':
				buf.WriteByte('\b')
			case 'f':
				buf.WriteByte('\f')
			case '\\', '(', ')':
				buf.WriteByte(next)
			case '\n':
				continue
			case '\r':
				if s.pos < len(s.data) && s.data[s.pos] == '\n' {
					s.pos++
				}
				continue
			default:
				if next >= '0' && next <= '7' {
					octal := []byte{next}
					for i := 0; i < 2 && s.pos < len(s.data); i++ {
						peek := s.data[s.pos]
						if peek < '0' || peek > '7' {
							break
						}
						octal = append(octal, peek)
						s.pos++
					}
					value, err := strconv.ParseInt(string(octal), 8, 8)
					if err != nil {
						return "", err
					}
					buf.WriteByte(byte(value))
				} else {
					buf.WriteByte(next)
				}
			}
			continue
		}
		switch ch {
		case '(':
			depth++
			buf.WriteByte(ch)
		case ')':
			depth--
			if depth == 0 {
				return buf.String(), nil
			}
			buf.WriteByte(ch)
		default:
			buf.WriteByte(ch)
		}
	}
	return "", errors.New("unterminated string literal")
}

func (s *contentScanner) parseHexString() (string, error) {
	if s.data[s.pos] != '<' {
		return "", errors.New("expected hex string")
	}
	s.pos++
	start := s.pos
	for s.pos < len(s.data) && s.data[s.pos] != '>' {
		s.pos++
	}
	if s.pos >= len(s.data) {
		return "", errors.New("unterminated hex string")
	}
	hexData := sanitizeHex(string(s.data[start:s.pos]))
	s.pos++
	if len(hexData)%2 == 1 {
		hexData += "0"
	}
	decoded := make([]byte, hex.DecodedLen(len(hexData)))
	if _, err := hex.Decode(decoded, []byte(hexData)); err != nil {
		return "", err
	}
	return decodePDFString(decoded), nil
}

func sanitizeHex(value string) string {
	var builder strings.Builder
	for _, ch := range value {
		if unicode.IsSpace(ch) {
			continue
		}
		builder.WriteRune(ch)
	}
	return builder.String()
}

func decodePDFString(data []byte) string {
	if len(data) >= 2 {
		if data[0] == 0xFE && data[1] == 0xFF {
			return decodeUTF16(data[2:], binary.BigEndian)
		}
		if data[0] == 0xFF && data[1] == 0xFE {
			return decodeUTF16(data[2:], binary.LittleEndian)
		}
	}
	return string(data)
}

func decodeUTF16(data []byte, order binary.ByteOrder) string {
	if len(data)%2 == 1 {
		data = data[:len(data)-1]
	}
	words := make([]uint16, len(data)/2)
	for i := 0; i < len(words); i++ {
		words[i] = order.Uint16(data[i*2 : i*2+2])
	}
	return string(utf16.Decode(words))
}

func (s *contentScanner) parseArray() ([]string, error) {
	if s.data[s.pos] != '[' {
		return nil, errors.New("expected array")
	}
	s.pos++
	values := make([]string, 0)
	for {
		if _, err := s.skipWhitespace(); err != nil {
			return nil, err
		}
		if s.pos >= len(s.data) {
			return nil, errors.New("unterminated array")
		}
		if s.data[s.pos] == ']' {
			s.pos++
			break
		}
		switch s.data[s.pos] {
		case '(':
			value, err := s.parseLiteralString()
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		case '<':
			if s.pos+1 < len(s.data) && s.data[s.pos+1] == '<' {
				s.skipDictionary()
				continue
			}
			value, err := s.parseHexString()
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		case '[':
			_, err := s.parseArray()
			if err != nil {
				return nil, err
			}
		default:
			s.readToken()
		}
	}
	return values, nil
}

func (s *contentScanner) skipDictionary() {
	if s.pos+1 >= len(s.data) {
		s.pos = len(s.data)
		return
	}
	s.pos += 2
	depth := 1
	for s.pos < len(s.data) && depth > 0 {
		if s.data[s.pos] == '<' && s.pos+1 < len(s.data) && s.data[s.pos+1] == '<' {
			depth++
			s.pos += 2
			continue
		}
		if s.data[s.pos] == '>' && s.pos+1 < len(s.data) && s.data[s.pos+1] == '>' {
			depth--
			s.pos += 2
			continue
		}
		s.pos++
	}
}

func (s *contentScanner) skipInlineImage() {
	idx := bytes.Index(s.data[s.pos:], []byte("EI"))
	if idx < 0 {
		s.pos = len(s.data)
		return
	}
	s.pos += idx + len("EI")
}

func isWhitespace(ch byte) bool {
	return ch == 0x00 || ch == 0x09 || ch == 0x0A || ch == 0x0C || ch == 0x0D || ch == 0x20
}

func isDelimiter(ch byte) bool {
	switch ch {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	default:
		return false
	}
}
