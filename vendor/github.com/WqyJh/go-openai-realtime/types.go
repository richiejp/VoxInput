package openairt

import (
	"encoding/json"
)

type Voice string

const (
	VoiceAlloy   Voice = "alloy"
	VoiceAsh     Voice = "ash"
	VoiceBallad  Voice = "ballad"
	VoiceCoral   Voice = "coral"
	VoiceEcho    Voice = "echo"
	VoiceSage    Voice = "sage"
	VoiceShimmer Voice = "shimmer"
	VoiceVerse   Voice = "verse"
)

type AudioFormat string

const (
	AudioFormatPcm16    AudioFormat = "pcm16"
	AudioFormatG711Ulaw AudioFormat = "g711_ulaw"
	AudioFormatG711Alaw AudioFormat = "g711_alaw"
)

type Modality string

const (
	ModalityText  Modality = "text"
	ModalityAudio Modality = "audio"
)

type ClientTurnDetectionType string

const (
	ClientTurnDetectionTypeServerVad ClientTurnDetectionType = "server_vad"
)

type ServerTurnDetectionType string

const (
	ServerTurnDetectionTypeNone      ServerTurnDetectionType = "none"
	ServerTurnDetectionTypeServerVad ServerTurnDetectionType = "server_vad"
)

type TurnDetectionType string

const (
	// TurnDetectionTypeNone means turn detection is disabled.
	// This can only be used in ServerSession, not in ClientSession.
	// If you want to disable turn detection, you should send SessionUpdateEvent with TurnDetection set to nil.
	TurnDetectionTypeNone TurnDetectionType = "none"
	// TurnDetectionTypeServerVad use server-side VAD to detect turn.
	// This is default value for newly created session.
	TurnDetectionTypeServerVad TurnDetectionType = "server_vad"
)

type TurnDetectionParams struct {
	// Activation threshold for VAD.
	Threshold float64 `json:"threshold,omitempty"`
	// Audio included before speech starts (in milliseconds).
	PrefixPaddingMs int `json:"prefix_padding_ms,omitempty"`
	// Duration of silence to detect speech stop (in milliseconds).
	SilenceDurationMs int `json:"silence_duration_ms,omitempty"`
	// Whether or not to automatically generate a response when VAD is enabled. true by default.
	CreateResponse *bool `json:"create_response,omitempty"`
}

type ClientTurnDetection struct {
	// Type of turn detection, only "server_vad" is currently supported.
	Type ClientTurnDetectionType `json:"type"`

	TurnDetectionParams
}

type ServerTurnDetection struct {
	// The type of turn detection ("server_vad" or "none").
	Type ServerTurnDetectionType `json:"type"`

	TurnDetectionParams
}

type ToolType string

const (
	ToolTypeFunction ToolType = "function"
)

type ToolChoiceInterface interface {
	ToolChoice()
}

type ToolChoiceString string

func (ToolChoiceString) ToolChoice() {}

const (
	ToolChoiceAuto     ToolChoiceString = "auto"
	ToolChoiceNone     ToolChoiceString = "none"
	ToolChoiceRequired ToolChoiceString = "required"
)

type ToolChoice struct {
	Type     ToolType     `json:"type"`
	Function ToolFunction `json:"function,omitempty"`
}

func (t ToolChoice) ToolChoice() {}

type ToolFunction struct {
	Name string `json:"name"`
}

type MessageRole string

const (
	MessageRoleSystem    MessageRole = "system"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleUser      MessageRole = "user"
)

type InputAudioTranscription struct {
	// The model used for transcription.
	Model    string `json:"model"`
	// The language of the input audio. Supplying the input language in ISO-639-1 (e.g. en) format will improve accuracy and latency.
	Language string `json:"language,omitempty"`
	// An optional text to guide the model's style or continue a previous audio segment. For whisper-1, the prompt is a list of keywords. For gpt-4o-transcribe models, the prompt is a free text string, for example "expect words related to technology".
	Prompt   string `json:"prompt,omitempty"`
}

type Tool struct {
	Type        ToolType `json:"type"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Parameters  any      `json:"parameters"`
}

type MessageItemType string

const (
	MessageItemTypeMessage            MessageItemType = "message"
	MessageItemTypeFunctionCall       MessageItemType = "function_call"
	MessageItemTypeFunctionCallOutput MessageItemType = "function_call_output"
)

type MessageContentType string

const (
	MessageContentTypeText       MessageContentType = "text"
	MessageContentTypeAudio      MessageContentType = "audio"
	MessageContentTypeTranscript MessageContentType = "transcript"
	MessageContentTypeInputText  MessageContentType = "input_text"
	MessageContentTypeInputAudio MessageContentType = "input_audio"
)

type MessageContentPart struct {
	// The content type.
	Type MessageContentType `json:"type"`
	// The text content. Validated if type is text.
	Text string `json:"text,omitempty"`
	// Base64-encoded audio data. Validated if type is audio.
	Audio string `json:"audio,omitempty"`
	// The transcript of the audio. Validated if type is transcript.
	Transcript string `json:"transcript,omitempty"`
}

type MessageItem struct {
	// The unique ID of the item.
	ID string `json:"id,omitempty"`
	// The type of the item ("message", "function_call", "function_call_output").
	Type MessageItemType `json:"type"`
	// The final status of the item.
	Status ItemStatus `json:"status,omitempty"`
	// The role associated with the item.
	Role MessageRole `json:"role,omitempty"`
	// The content of the item.
	Content []MessageContentPart `json:"content,omitempty"`
	// The ID of the function call, if the item is a function call.
	CallID string `json:"call_id,omitempty"`
	// The name of the function, if the item is a function call.
	Name string `json:"name,omitempty"`
	// The arguments of the function, if the item is a function call.
	Arguments string `json:"arguments,omitempty"`
	// The output of the function, if the item is a function call output.
	Output string `json:"output,omitempty"`
}

type ResponseMessageItem struct {
	MessageItem
	// The object type, must be "realtime.item".
	Object string `json:"object,omitempty"`
}

type Error struct {
	// The type of error (e.g., "invalid_request_error", "server_error").
	Message string `json:"message,omitempty"`
	// Error code, if any.
	Type string `json:"type,omitempty"`
	// A human-readable error message.
	Code string `json:"code,omitempty"`
	// Parameter related to the error, if any.
	Param string `json:"param,omitempty"`
	// The event_id of the client event that caused the error, if applicable.
	EventID string `json:"event_id,omitempty"`
}

// ServerToolChoice is a type that can be used to choose a tool response from the server.
type ServerToolChoice struct {
	String   ToolChoiceString
	Function ToolChoice
}

// UnmarshalJSON is a custom unmarshaler for ServerToolChoice.
func (m *ServerToolChoice) UnmarshalJSON(data []byte) error {
	err := json.Unmarshal(data, &m.Function)
	if err != nil {
		if data[0] == '"' {
			data = data[1:]
		}
		if data[len(data)-1] == '"' {
			data = data[:len(data)-1]
		}
		m.String = ToolChoiceString(data)
		m.Function = ToolChoice{}
		return nil
	}
	return nil
}

// IsFunction returns true if the tool choice is a function call.
func (m *ServerToolChoice) IsFunction() bool {
	return m.Function.Type == ToolTypeFunction
}

// Get returns the ToolChoiceInterface based on the type of tool choice.
func (m ServerToolChoice) Get() ToolChoiceInterface {
	if m.IsFunction() {
		return m.Function
	}
	return m.String
}

type ServerSession struct {
	// The unique ID of the session.
	ID string `json:"id"`
	// The object type, must be "realtime.session".
	Object string `json:"object"`
	// The default model used for this session.
	Model string `json:"model"`
	// The set of modalities the model can respond with.
	Modalities []Modality `json:"modalities,omitempty"`
	// The default system instructions.
	Instructions string `json:"instructions,omitempty"`
	// The voice the model uses to respond - one of alloy, echo, or shimmer.
	Voice Voice `json:"voice,omitempty"`
	// The format of input audio.
	InputAudioFormat AudioFormat `json:"input_audio_format,omitempty"`
	// The format of output audio.
	OutputAudioFormat AudioFormat `json:"output_audio_format,omitempty"`
	// Configuration for input audio transcription.
	InputAudioTranscription *InputAudioTranscription `json:"input_audio_transcription,omitempty"`
	// Configuration for turn detection.
	TurnDetection *ServerTurnDetection `json:"turn_detection,omitempty"`
	// Tools (functions) available to the model.
	Tools []Tool `json:"tools,omitempty"`
	// How the model chooses tools.
	ToolChoice ServerToolChoice `json:"tool_choice,omitempty"`
	// Sampling temperature.
	Temperature *float32 `json:"temperature,omitempty"`
	// Maximum number of output tokens.
	MaxOutputTokens IntOrInf `json:"max_response_output_tokens,omitempty"`
}

type ItemStatus string

const (
	ItemStatusInProgress ItemStatus = "in_progress"
	ItemStatusCompleted  ItemStatus = "completed"
	ItemStatusIncomplete ItemStatus = "incomplete"
)

type Conversation struct {
	// The unique ID of the conversation.
	ID string `json:"id"`
	// The object type, must be "realtime.conversation".
	Object string `json:"object"`
}

type ResponseStatus string

const (
	ResponseStatusInProgress ResponseStatus = "in_progress"
	ResponseStatusCompleted  ResponseStatus = "completed"
	ResponseStatusCancelled  ResponseStatus = "cancelled"
	ResponseStatusIncomplete ResponseStatus = "incomplete"
	ResponseStatusFailed     ResponseStatus = "failed"
)

type CachedTokensDetails struct {
	TextTokens  int `json:"text_tokens"`
	AudioTokens int `json:"audio_tokens"`
}

type InputTokenDetails struct {
	CachedTokens        int                 `json:"cached_tokens"`
	TextTokens          int                 `json:"text_tokens"`
	AudioTokens         int                 `json:"audio_tokens"`
	CachedTokensDetails CachedTokensDetails `json:"cached_tokens_details,omitempty"`
}

type OutputTokenDetails struct {
	TextTokens  int `json:"text_tokens"`
	AudioTokens int `json:"audio_tokens"`
}

type Usage struct {
	TotalTokens  int `json:"total_tokens"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	// Input token details.
	InputTokenDetails InputTokenDetails `json:"input_token_details,omitempty"`
	// Output token details.
	OutputTokenDetails OutputTokenDetails `json:"output_token_details,omitempty"`
}

type Response struct {
	// The unique ID of the response.
	ID string `json:"id"`
	// The object type, must be "realtime.response".
	Object string `json:"object"`
	// The status of the response.
	Status ResponseStatus `json:"status"`
	// Additional details about the status.
	StatusDetails any `json:"status_details,omitempty"`
	// The list of output items generated by the response.
	Output []ResponseMessageItem `json:"output"`
	// Usage statistics for the response.
	Usage *Usage `json:"usage,omitempty"`
}

type RateLimit struct {
	// The name of the rate limit ("requests", "tokens", "input_tokens", "output_tokens").
	Name string `json:"name"`
	// The maximum allowed value for the rate limit.
	Limit int `json:"limit"`
	// The remaining value before the limit is reached.
	Remaining int `json:"remaining"`
	// Seconds until the rate limit resets.
	ResetSeconds float64 `json:"reset_seconds"`
}
