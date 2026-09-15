package control

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	app_errors "gpt-load/internal/platform/errors"
)

// donationUsage is the bounded usage projection. It is parsed from the client
// protocol wire format so native and converted routes report the same shape.
type donationUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// donationRedactor removes known secret byte sequences from a text stream. The
// secret can be echoed across network chunk boundaries, so a plain per-chunk
// replacement is insufficient: the trailing maxSecret-1 bytes are always held
// back until enough following bytes arrive to prove no occurrence straddles the
// cut.
type donationRedactor struct {
	secrets     [][]byte
	replacement []byte
	pending     []byte
}

const donationRedactionMarker = "[redacted]"

func newDonationRedactor(secrets ...string) *donationRedactor {
	redactor := &donationRedactor{replacement: []byte(donationRedactionMarker)}
	seen := make(map[string]bool, len(secrets))
	for _, secret := range secrets {
		if len(secret) == 0 || seen[secret] {
			continue
		}
		seen[secret] = true
		redactor.secrets = append(redactor.secrets, []byte(secret))
	}
	return redactor
}

func (r *donationRedactor) maxSecret() int {
	longest := 0
	for _, secret := range r.secrets {
		if len(secret) > longest {
			longest = len(secret)
		}
	}
	return longest
}

// Write appends a chunk and returns the prefix that is provably free of any
// complete secret occurrence. Callers must emit exactly the returned bytes.
func (r *donationRedactor) Write(chunk []byte) []byte {
	r.pending = append(r.pending, chunk...)
	if len(r.secrets) == 0 {
		emitted := r.pending
		r.pending = nil
		return emitted
	}
	keep := r.maxSecret() - 1
	if len(r.pending) <= keep {
		return nil
	}
	return r.drain(len(r.pending) - keep)
}

// Flush returns the remaining buffered text. It is only valid once the stream is
// known to have ended.
func (r *donationRedactor) Flush() []byte { return r.drain(len(r.pending)) }

func (r *donationRedactor) drain(limit int) []byte {
	if limit <= 0 {
		return nil
	}
	out := make([]byte, 0, limit)
	index := 0
	for index < limit {
		if length := r.matchLength(index); length > 0 {
			out = append(out, r.replacement...)
			index += length
			continue
		}
		_, width := utf8.DecodeRune(r.pending[index:])
		if index+width > limit {
			break
		}
		out = append(out, r.pending[index:index+width]...)
		index += width
	}
	remaining := append([]byte(nil), r.pending[index:]...)
	r.pending = remaining
	return out
}

func (r *donationRedactor) matchLength(index int) int {
	for _, secret := range r.secrets {
		if index+len(secret) > len(r.pending) {
			continue
		}
		if bytes.Equal(r.pending[index:index+len(secret)], secret) {
			return len(secret)
		}
	}
	return 0
}

// sseFrameDecoder splits an SSE byte stream into complete frames across network
// chunk boundaries. It never forwards the raw upstream bytes.
type sseFrameDecoder struct {
	buffer []byte
}

func (d *sseFrameDecoder) Feed(chunk []byte, maxEvent int) ([][]byte, error) {
	d.buffer = append(d.buffer, chunk...)
	frames := make([][]byte, 0, 1)
	for {
		index := indexSSEFrameBoundary(d.buffer)
		if index < 0 {
			if len(d.buffer) > maxEvent {
				return nil, errDonationResponseTooLarge
			}
			return frames, nil
		}
		if index > maxEvent {
			return nil, errDonationResponseTooLarge
		}
		frames = append(frames, append([]byte(nil), d.buffer[:index]...))
		d.buffer = append([]byte(nil), d.buffer[index:]...)
	}
}

func indexSSEFrameBoundary(buffer []byte) int {
	for index := 0; index < len(buffer); index++ {
		if buffer[index] != '\n' {
			continue
		}
		if index+1 < len(buffer) && buffer[index+1] == '\n' {
			return index + 2
		}
		// A frame may end with CRLF line endings.
		if index >= 2 && buffer[index-1] == '\r' && index+1 < len(buffer) && buffer[index+1] == '\r' && index+2 < len(buffer) && buffer[index+2] == '\n' {
			return index + 3
		}
	}
	return -1
}

// sseDataPayload extracts the joined data lines of one SSE frame. Field names
// other than "data" are ignored, so upstream event metadata never reaches a
// browser.
func sseDataPayload(frame []byte) ([]byte, bool) {
	var joined []byte
	found := false
	for _, line := range strings.Split(strings.ReplaceAll(string(frame), "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		value := strings.TrimPrefix(line, "data:")
		value = strings.TrimPrefix(value, " ")
		if found {
			joined = append(joined, '\n')
		}
		joined = append(joined, value...)
		found = true
	}
	return joined, found
}

type donationChatChunk struct {
	Text      string
	Reasoning string
	Usage     *donationUsage
	Done      bool
}

// decodeDonationChatChunk reads only the fields the contract allows to leave
// gpt-load. Unknown provider fields, tool calls and raw metadata are dropped.
func decodeDonationChatChunk(payload []byte) (donationChatChunk, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return donationChatChunk{}, nil
	}
	if bytes.Equal(trimmed, []byte("[DONE]")) {
		return donationChatChunk{Done: true}, nil
	}
	var envelope struct {
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			Delta struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"delta"`
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return donationChatChunk{}, app_errors.ErrBadGateway
	}
	if (len(envelope.Error) > 0 && !bytes.Equal(bytes.TrimSpace(envelope.Error), []byte("null"))) ||
		(len(envelope.Choices) == 0 && envelope.Usage == nil) {
		return donationChatChunk{}, app_errors.ErrBadGateway
	}
	chunk := donationChatChunk{}
	for _, choice := range envelope.Choices {
		chunk.Text += choice.Delta.Content
		chunk.Reasoning += choice.Delta.ReasoningContent
		if chunk.Text == "" {
			chunk.Text = choice.Message.Content
		}
		if chunk.Reasoning == "" {
			chunk.Reasoning = choice.Message.ReasoningContent
		}
	}
	if envelope.Usage != nil {
		if envelope.Usage.PromptTokens < 0 || envelope.Usage.CompletionTokens < 0 {
			return donationChatChunk{}, app_errors.ErrBadGateway
		}
		chunk.Usage = &donationUsage{InputTokens: envelope.Usage.PromptTokens,
			OutputTokens: envelope.Usage.CompletionTokens}
	}
	return chunk, nil
}

// donationStreamProjection turns raw executor stream frames into the bounded,
// redacted event payloads the integration contract exposes.
type donationStreamProjection struct {
	decoder       sseFrameDecoder
	redactor      *donationRedactor
	maxResponse   int
	maxEvent      int
	emittedBytes  int
	usage         *donationUsage
	sawCompletion bool
	visibleOutput bool
}

func newDonationStreamProjection(secrets []string) *donationStreamProjection {
	return &donationStreamProjection{redactor: newDonationRedactor(secrets...),
		maxResponse: donationMaxResponseBytes, maxEvent: donationMaxEventBytes}
}

// donationProjectedEvent is one contract-level delta. Text already passed the
// redactor; an empty Text means nothing safe could be forwarded yet.
type donationProjectedEvent struct {
	Text string
	Done bool
	ctx  context.Context
}

// Push consumes one raw frame and returns the deltas that may be forwarded.
func (p *donationStreamProjection) Push(frame []byte) ([]donationProjectedEvent, error) {
	frames, err := p.decoder.Feed(frame, p.maxEvent)
	if err != nil {
		return nil, err
	}
	events := make([]donationProjectedEvent, 0, 1)
	for _, complete := range frames {
		payload, ok := sseDataPayload(complete)
		if !ok {
			continue
		}
		if p.sawCompletion {
			return nil, app_errors.ErrBadGateway
		}
		chunk, err := decodeDonationChatChunk(payload)
		if err != nil {
			return nil, err
		}
		if chunk.Usage != nil {
			p.usage = chunk.Usage
		}
		if chunk.Done {
			p.sawCompletion = true
			continue
		}
		text := chunk.Text + chunk.Reasoning
		if text == "" {
			continue
		}
		safe := p.redactor.Write([]byte(text))
		if len(safe) == 0 {
			continue
		}
		if p.emittedBytes+len(safe) > p.maxResponse {
			return nil, errDonationResponseTooLarge
		}
		p.emittedBytes += len(safe)
		p.visibleOutput = p.visibleOutput || strings.TrimSpace(string(safe)) != ""
		events = append(events, donationTextEvents(string(safe))...)
	}
	return events, nil
}

// Finish flushes the held-back tail. It is only called after the stream ended so
// no partial secret can be released early.
func (p *donationStreamProjection) Finish() (string, error) {
	if len(bytes.TrimSpace(p.decoder.buffer)) > 0 {
		p.sawCompletion = false
		return "", app_errors.ErrBadGateway
	}
	tail := p.redactor.Flush()
	if len(tail) == 0 {
		return "", nil
	}
	if p.emittedBytes+len(tail) > p.maxResponse {
		return "", errDonationResponseTooLarge
	}
	p.emittedBytes += len(tail)
	p.visibleOutput = p.visibleOutput || strings.TrimSpace(string(tail)) != ""
	return string(tail), nil
}

func (p *donationStreamProjection) Usage() *donationUsage { return p.usage }

func (p *donationStreamProjection) Visible() bool { return p.visibleOutput }

func (p *donationStreamProjection) Complete() bool { return p.sawCompletion }

// A JSON string can expand sixfold (for example '<' -> '\u003c'). Split text at
// rune boundaries before writing so the encoded contract event remains bounded.
func donationTextEvents(text string) []donationProjectedEvent {
	const maxText = (donationMaxEventBytes - 64) / 6
	var events []donationProjectedEvent
	for text != "" {
		length := min(len(text), maxText)
		for length < len(text) && !utf8.RuneStart(text[length]) {
			length--
		}
		events = append(events, donationProjectedEvent{Text: text[:length]})
		text = text[length:]
	}
	return events
}

// projectDonationChatBody applies the same redaction and field whitelist to a
// non-streaming response body.
func projectDonationChatBody(body []byte, secrets []string) (string, *donationUsage, bool, error) {
	if len(body) > donationMaxResponseBytes {
		return "", nil, false, errDonationResponseTooLarge
	}
	chunk, err := decodeDonationChatChunk(body)
	if err != nil {
		return "", nil, false, err
	}
	redactor := newDonationRedactor(secrets...)
	text := string(redactor.Write([]byte(chunk.Text + chunk.Reasoning)))
	text += string(redactor.Flush())
	if len(text) > donationMaxResponseBytes {
		return "", nil, false, errDonationResponseTooLarge
	}
	return text, chunk.Usage, strings.TrimSpace(text) != "", nil
}
