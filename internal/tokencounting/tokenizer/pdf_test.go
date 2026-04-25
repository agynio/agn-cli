package tokenizer

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/stretchr/testify/require"
)

func TestCountPDFTokens(t *testing.T) {
	pdfData := buildTestPDF(t, "Hello PDF")

	text, dims, err := extractPDFText(pdfData)
	require.NoError(t, err)
	require.Contains(t, text, "Hello PDF")
	require.Len(t, dims, 1)
	require.Greater(t, dims[0].Width, 0.0)
	require.Greater(t, dims[0].Height, 0.0)

	counter, err := newCounter(ModelGPT5)
	require.NoError(t, err)

	tokens, err := counter.countPDFTokens(pdfData)
	require.NoError(t, err)
	require.Greater(t, tokens, 0)
}

func buildTestPDF(t *testing.T, text string) []byte {
	t.Helper()
	spec := fmt.Sprintf(`{"paper":"A4P","origin":"LowerLeft","pages":{"1":{"content":{"text":[{"value":%q,"pos":[50,700],"font":{"name":"Helvetica","size":12}}]}}}}`, text)
	var buf bytes.Buffer
	err := api.Create(nil, strings.NewReader(spec), &buf, model.NewDefaultConfiguration())
	require.NoError(t, err)
	return buf.Bytes()
}
