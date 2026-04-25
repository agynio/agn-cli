package tokenizer

type ItemType string

const (
	ItemTypeMessage            ItemType = "message"
	ItemTypeFunctionCall       ItemType = "function_call"
	ItemTypeFunctionCallOutput ItemType = "function_call_output"
)

type Item interface {
	itemType() ItemType
}

type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

type MessageItem struct {
	Role    MessageRole
	Content []ContentPart
}

func (MessageItem) itemType() ItemType { return ItemTypeMessage }

type FunctionCallItem struct {
	Arguments string
}

func (FunctionCallItem) itemType() ItemType { return ItemTypeFunctionCall }

type FunctionCallOutputItem struct {
	Output FunctionCallOutput
}

func (FunctionCallOutputItem) itemType() ItemType { return ItemTypeFunctionCallOutput }

type FunctionCallOutput struct {
	Text    string
	Content []ContentPart
	IsText  bool
}

type ContentType string

const (
	ContentTypeInputText  ContentType = "input_text"
	ContentTypeOutputText ContentType = "output_text"
	ContentTypeRefusal    ContentType = "refusal"
	ContentTypeInputImage ContentType = "input_image"
	ContentTypeInputFile  ContentType = "input_file"
	ContentTypeInputAudio ContentType = "input_audio"
)

type ContentPart struct {
	Type  ContentType
	Text  string
	Image ImageContent
	File  FileContent
}

type ImageDetail string

const (
	ImageDetailHigh ImageDetail = "high"
	ImageDetailLow  ImageDetail = "low"
)

type ImageContent struct {
	ImageURL string
	FileID   string
	Detail   ImageDetail
}

type FileContent struct {
	Filename string
	Data     string
}
