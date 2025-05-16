package openairt

import "encoding/json"

// ClientEventType is the type of client event. See https://platform.openai.com/docs/guides/realtime/client-events
type ClientEventType string

const (
	ClientEventTypeSessionUpdate              ClientEventType = "session.update"
	ClientEventTypeTranscriptionSessionUpdate ClientEventType = "transcription_session.update"
	ClientEventTypeInputAudioBufferAppend     ClientEventType = "input_audio_buffer.append"
	ClientEventTypeInputAudioBufferCommit     ClientEventType = "input_audio_buffer.commit"
	ClientEventTypeInputAudioBufferClear      ClientEventType = "input_audio_buffer.clear"
	ClientEventTypeConversationItemCreate     ClientEventType = "conversation.item.create"
	ClientEventTypeConversationItemTruncate   ClientEventType = "conversation.item.truncate"
	ClientEventTypeConversationItemDelete     ClientEventType = "conversation.item.delete"
	ClientEventTypeResponseCreate             ClientEventType = "response.create"
	ClientEventTypeResponseCancel             ClientEventType = "response.cancel"
)

// ClientEvent is the interface for client event.
type ClientEvent interface {
	ClientEventType() ClientEventType
}

// EventBase is the base struct for all client events.
type EventBase struct {
	// Optional client-generated ID used to identify this event.
	EventID string `json:"event_id,omitempty"`
}

type ClientSession struct {
	// The set of modalities the model can respond with. To disable audio, set this to ["text"].
	Modalities []Modality `json:"modalities,omitempty"`
	// The default system instructions prepended to model calls.
	Instructions string `json:"instructions,omitempty"`
	// The voice the model uses to respond - one of alloy, echo, or shimmer. Cannot be changed once the model has responded with audio at least once.
	Voice Voice `json:"voice,omitempty"`
	// The format of input audio. Options are "pcm16", "g711_ulaw", or "g711_alaw".
	InputAudioFormat AudioFormat `json:"input_audio_format,omitempty"`
	// The format of output audio. Options are "pcm16", "g711_ulaw", or "g711_alaw".
	OutputAudioFormat AudioFormat `json:"output_audio_format,omitempty"`
	// Configuration for input audio transcription. Can be set to `nil` to turn off.
	InputAudioTranscription *InputAudioTranscription `json:"input_audio_transcription,omitempty"`
	// Configuration for turn detection. Can be set to `nil` to turn off.
	TurnDetection *ClientTurnDetection `json:"turn_detection"`
	// Tools (functions) available to the model.
	Tools []Tool `json:"tools,omitempty"`
	// How the model chooses tools. Options are "auto", "none", "required", or specify a function.
	ToolChoice ToolChoiceInterface `json:"tool_choice,omitempty"`
	// Sampling temperature for the model.
	Temperature *float32 `json:"temperature,omitempty"`
	// Maximum number of output tokens for a single assistant response, inclusive of tool calls. Provide an integer between 1 and 4096 to limit output tokens, or "inf" for the maximum available tokens for a given model. Defaults to "inf".
	MaxOutputTokens IntOrInf `json:"max_response_output_tokens,omitempty"`
}

// SessionUpdateEvent is the event for session update.
// Send this event to update the session’s default configuration.
// See https://platform.openai.com/docs/api-reference/realtime-client-events/session/update
type SessionUpdateEvent struct {
	EventBase
	// Session configuration to update.
	Session ClientSession `json:"session"`
}

func (m SessionUpdateEvent) ClientEventType() ClientEventType {
	return ClientEventTypeSessionUpdate
}

func (m SessionUpdateEvent) MarshalJSON() ([]byte, error) {
	type sessionUpdateEvent SessionUpdateEvent
	v := struct {
		*sessionUpdateEvent
		Type ClientEventType `json:"type"`
	}{
		sessionUpdateEvent: (*sessionUpdateEvent)(&m),
		Type:               m.ClientEventType(),
	}
	return json.Marshal(v)
}

type NoiseReductionType string

const (
	NearFieldNoiseReduction NoiseReductionType = "near_field"
	FarFieldNoiseReduction  NoiseReductionType = "far_field"
)

type InputAudioNoiseReduction struct {
	// Type of noise reduction. near_field is for close-talking microphones such as headphones, far_field is for far-field microphones such as laptop or conference room microphones.
	Type NoiseReductionType `json:"type"`
}

type ClientTranscriptionSession struct {
	Include []string `json:"include,omitempty"`
	// The set of modalities the model can respond with. To disable audio, set this to ["text"].
	Modalities []Modality `json:"modalities,omitempty"`
	// The format of input audio. Options are "pcm16", "g711_ulaw", or "g711_alaw".
	InputAudioFormat AudioFormat `json:"input_audio_format,omitempty"`
	// Configuration for input audio noise reduction. This can be set to null to turn off. Noise reduction filters audio added to the input audio buffer before it is sent to VAD and the model. Filtering the audio can improve VAD and turn detection accuracy (reducing false positives) and model performance by improving perception of the input audio.
	InputAudioNoiseReduction *InputAudioNoiseReduction `json:"input_audio_noise_reduction,omitempty"`
	// Configuration for input audio transcription. Can be set to `nil` to turn off.
	InputAudioTranscription *InputAudioTranscription `json:"input_audio_transcription,omitempty"`
	// Configuration for turn detection. Can be set to `nil` to turn off.
	TurnDetection *ClientTurnDetection `json:"turn_detection"`
}

// SessionUpdateEvent is the event for session update.
// Send this event to update the session’s default configuration.
// See https://platform.openai.com/docs/api-reference/realtime-client-events/session/update
type TranscriptionSessionUpdateEvent struct {
	EventBase
	// Session configuration to update.
	Session ClientTranscriptionSession `json:"session"`
}

func (m TranscriptionSessionUpdateEvent) ClientEventType() ClientEventType {
	return ClientEventTypeTranscriptionSessionUpdate
}

func (m TranscriptionSessionUpdateEvent) MarshalJSON() ([]byte, error) {
	type sessionUpdateEvent TranscriptionSessionUpdateEvent
	v := struct {
		*sessionUpdateEvent
		Type ClientEventType `json:"type"`
	}{
		sessionUpdateEvent: (*sessionUpdateEvent)(&m),
		Type:               m.ClientEventType(),
	}
	return json.Marshal(v)
}

// InputAudioBufferAppendEvent is the event for input audio buffer append.
// Send this event to append audio bytes to the input audio buffer.
// See https://platform.openai.com/docs/api-reference/realtime-client-events/input_audio_buffer/append
type InputAudioBufferAppendEvent struct {
	EventBase
	Audio string `json:"audio"` // Base64-encoded audio bytes.
}

func (m InputAudioBufferAppendEvent) ClientEventType() ClientEventType {
	return ClientEventTypeInputAudioBufferAppend
}

func (m InputAudioBufferAppendEvent) MarshalJSON() ([]byte, error) {
	type inputAudioBufferAppendEvent InputAudioBufferAppendEvent
	v := struct {
		*inputAudioBufferAppendEvent
		Type ClientEventType `json:"type"`
	}{
		inputAudioBufferAppendEvent: (*inputAudioBufferAppendEvent)(&m),
		Type:                        m.ClientEventType(),
	}
	return json.Marshal(v)
}

// InputAudioBufferCommitEvent is the event for input audio buffer commit.
// Send this event to commit audio bytes to a user message.
// See https://platform.openai.com/docs/api-reference/realtime-client-events/input_audio_buffer/commit
type InputAudioBufferCommitEvent struct {
	EventBase
}

func (m InputAudioBufferCommitEvent) ClientEventType() ClientEventType {
	return ClientEventTypeInputAudioBufferCommit
}

func (m InputAudioBufferCommitEvent) MarshalJSON() ([]byte, error) {
	type inputAudioBufferCommitEvent InputAudioBufferCommitEvent
	v := struct {
		*inputAudioBufferCommitEvent
		Type ClientEventType `json:"type"`
	}{
		inputAudioBufferCommitEvent: (*inputAudioBufferCommitEvent)(&m),
		Type:                        m.ClientEventType(),
	}
	return json.Marshal(v)
}

// InputAudioBufferClearEvent is the event for input audio buffer clear.
// Send this event to clear the audio bytes in the buffer.
// See https://platform.openai.com/docs/api-reference/realtime-client-events/input_audio_buffer/clear
type InputAudioBufferClearEvent struct {
	EventBase
}

func (m InputAudioBufferClearEvent) ClientEventType() ClientEventType {
	return ClientEventTypeInputAudioBufferClear
}

func (m InputAudioBufferClearEvent) MarshalJSON() ([]byte, error) {
	type inputAudioBufferClearEvent InputAudioBufferClearEvent
	v := struct {
		*inputAudioBufferClearEvent
		Type ClientEventType `json:"type"`
	}{
		inputAudioBufferClearEvent: (*inputAudioBufferClearEvent)(&m),
		Type:                       m.ClientEventType(),
	}
	return json.Marshal(v)
}

// ConversationItemCreateEvent is the event for conversation item create.
// Send this event when adding an item to the conversation.
// See https://platform.openai.com/docs/api-reference/realtime-client-events/conversation/item/create
type ConversationItemCreateEvent struct {
	EventBase
	// The ID of the preceding item after which the new item will be inserted.
	PreviousItemID string `json:"previous_item_id,omitempty"`
	// The item to add to the conversation.
	Item MessageItem `json:"item"`
}

func (m ConversationItemCreateEvent) ClientEventType() ClientEventType {
	return ClientEventTypeConversationItemCreate
}

func (m ConversationItemCreateEvent) MarshalJSON() ([]byte, error) {
	type conversationItemCreateEvent ConversationItemCreateEvent
	v := struct {
		*conversationItemCreateEvent
		Type ClientEventType `json:"type"`
	}{
		conversationItemCreateEvent: (*conversationItemCreateEvent)(&m),
		Type:                        m.ClientEventType(),
	}
	return json.Marshal(v)
}

// ConversationItemTruncateEvent is the event for conversation item truncate.
// Send this event when you want to truncate a previous assistant message’s audio.
// See https://platform.openai.com/docs/api-reference/realtime-client-events/conversation/item/truncate
type ConversationItemTruncateEvent struct {
	EventBase
	// The ID of the assistant message item to truncate.
	ItemID string `json:"item_id"`
	// The index of the content part to truncate.
	ContentIndex int `json:"content_index"`
	// Inclusive duration up to which audio is truncated, in milliseconds.
	AudioEndMs int `json:"audio_end_ms"`
}

func (m ConversationItemTruncateEvent) ClientEventType() ClientEventType {
	return ClientEventTypeConversationItemTruncate
}

func (m ConversationItemTruncateEvent) MarshalJSON() ([]byte, error) {
	type conversationItemTruncateEvent ConversationItemTruncateEvent
	v := struct {
		*conversationItemTruncateEvent
		Type ClientEventType `json:"type"`
	}{
		conversationItemTruncateEvent: (*conversationItemTruncateEvent)(&m),
		Type:                          m.ClientEventType(),
	}
	return json.Marshal(v)
}

// ConversationItemDeleteEvent is the event for conversation item delete.
// Send this event when you want to remove any item from the conversation history.
// See https://platform.openai.com/docs/api-reference/realtime-client-events/conversation/item/delete
type ConversationItemDeleteEvent struct {
	EventBase
	// The ID of the item to delete.
	ItemID string `json:"item_id"`
}

func (m ConversationItemDeleteEvent) ClientEventType() ClientEventType {
	return ClientEventTypeConversationItemDelete
}

func (m ConversationItemDeleteEvent) MarshalJSON() ([]byte, error) {
	type conversationItemDeleteEvent ConversationItemDeleteEvent
	v := struct {
		*conversationItemDeleteEvent
		Type ClientEventType `json:"type"`
	}{
		conversationItemDeleteEvent: (*conversationItemDeleteEvent)(&m),
		Type:                        m.ClientEventType(),
	}
	return json.Marshal(v)
}

type ResponseCreateParams struct {
	// The modalities for the response.
	Modalities []Modality `json:"modalities,omitempty"`
	// Instructions for the model.
	Instructions string `json:"instructions,omitempty"`
	// The voice the model uses to respond - one of alloy, echo, or shimmer.
	Voice Voice `json:"voice,omitempty"`
	// The format of output audio.
	OutputAudioFormat AudioFormat `json:"output_audio_format,omitempty"`
	// Tools (functions) available to the model.
	Tools []Tool `json:"tools,omitempty"`
	// How the model chooses tools.
	ToolChoice ToolChoiceInterface `json:"tool_choice,omitempty"`
	// Sampling temperature.
	Temperature *float32 `json:"temperature,omitempty"`
	// Maximum number of output tokens for a single assistant response, inclusive of tool calls. Provide an integer between 1 and 4096 to limit output tokens, or "inf" for the maximum available tokens for a given model. Defaults to "inf".
	MaxOutputTokens IntOrInf `json:"max_output_tokens,omitempty"`
}

// ResponseCreateEvent is the event for response create.
// Send this event to trigger a response generation.
// See https://platform.openai.com/docs/api-reference/realtime-client-events/response/create
type ResponseCreateEvent struct {
	EventBase
	// Configuration for the response.
	Response ResponseCreateParams `json:"response"`
}

func (m ResponseCreateEvent) ClientEventType() ClientEventType {
	return ClientEventTypeResponseCreate
}

func (m ResponseCreateEvent) MarshalJSON() ([]byte, error) {
	type responseCreateEvent ResponseCreateEvent
	v := struct {
		*responseCreateEvent
		Type ClientEventType `json:"type"`
	}{
		responseCreateEvent: (*responseCreateEvent)(&m),
		Type:                m.ClientEventType(),
	}
	return json.Marshal(v)
}

// ResponseCancelEvent is the event for response cancel.
// Send this event to cancel an in-progress response.
// See https://platform.openai.com/docs/api-reference/realtime-client-events/response/cancel
type ResponseCancelEvent struct {
	EventBase
	// A specific response ID to cancel - if not provided, will cancel an in-progress response in the default conversation.
	ResponseID string `json:"response_id,omitempty"`
}

func (m ResponseCancelEvent) ClientEventType() ClientEventType {
	return ClientEventTypeResponseCancel
}

func (m ResponseCancelEvent) MarshalJSON() ([]byte, error) {
	type responseCancelEvent ResponseCancelEvent
	v := struct {
		*responseCancelEvent
		Type ClientEventType `json:"type"`
	}{
		responseCancelEvent: (*responseCancelEvent)(&m),
		Type:                m.ClientEventType(),
	}
	return json.Marshal(v)
}

// MarshalClientEvent marshals the client event to JSON.
func MarshalClientEvent(event ClientEvent) ([]byte, error) {
	return json.Marshal(event)
}
