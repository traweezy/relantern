package delivery

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/traweezy/relantern/internal/digest"
)

const maximumResponseBytes = 64 << 10

type Config struct {
	Mode           string
	CaptureURL     string
	DiscordURL     string
	ResendURL      string
	ResendAPIKey   string
	EmailFrom      string
	EmailTo        string
	PublicBaseURL  string
	RequestTimeout time.Duration
}

type Client struct {
	config Config
	http   *http.Client
}

type PermanentError struct {
	err error
}

func (err *PermanentError) Error() string {
	return err.err.Error()
}

func (err *PermanentError) Unwrap() error {
	return err.err
}

func Permanent(err error) bool {
	var permanent *PermanentError
	return errors.As(err, &permanent)
}

func New(config Config) (*Client, error) {
	if config.RequestTimeout <= 0 || config.RequestTimeout > 30*time.Second {
		return nil, errors.New("delivery request timeout must be positive and at most 30 seconds")
	}
	if config.Mode != "log" && config.Mode != "live" && config.Mode != "disabled" {
		return nil, errors.New("delivery mode must be log, live, or disabled")
	}
	if config.Mode == "log" && strings.TrimSpace(config.CaptureURL) == "" {
		return nil, errors.New("log delivery requires a capture URL")
	}
	if config.Mode == "live" && strings.TrimSpace(config.PublicBaseURL) == "" {
		return nil, errors.New("live delivery requires the public application URL")
	}
	return &Client{config: config, http: &http.Client{Timeout: config.RequestTimeout}}, nil
}

func (client *Client) Send(ctx context.Context, request digest.Delivery) (digest.Receipt, error) {
	if request.Channel != digest.ChannelDiscord && request.Channel != digest.ChannelEmail {
		return digest.Receipt{}, &PermanentError{err: errors.New("only Discord and email use external delivery")}
	}
	if request.IdempotencyKey == "" || request.DigestID == "" || request.Attempt < 1 {
		return digest.Receipt{}, &PermanentError{err: errors.New("delivery request is incomplete")}
	}
	switch client.config.Mode {
	case "disabled":
		return digest.Receipt{}, &PermanentError{err: errors.New("external delivery is disabled")}
	case "log":
		return client.sendCapture(ctx, request)
	case "live":
		if request.Channel == digest.ChannelDiscord {
			return client.sendDiscord(ctx, request)
		}
		return client.sendEmail(ctx, request)
	default:
		return digest.Receipt{}, &PermanentError{err: errors.New("delivery mode is invalid")}
	}
}

func (client *Client) sendCapture(ctx context.Context, request digest.Delivery) (digest.Receipt, error) {
	payload := struct {
		Attempt        int                  `json:"attempt"`
		Channel        string               `json:"channel"`
		DigestID       string               `json:"digestId"`
		IdempotencyKey string               `json:"idempotencyKey"`
		LocalDate      string               `json:"localDate"`
		Payload        digest.DigestPayload `json:"payload"`
		PayloadSHA256  string               `json:"payloadSha256"`
		ScheduledFor   time.Time            `json:"scheduledFor"`
	}{
		Attempt: request.Attempt, Channel: request.Channel, DigestID: request.DigestID,
		IdempotencyKey: request.IdempotencyKey, LocalDate: request.LocalDate,
		Payload: request.Payload, PayloadSHA256: hex.EncodeToString(request.PayloadSHA256),
		ScheduledFor: request.ScheduledFor.UTC(),
	}
	providerID, err := client.postJSON(ctx, client.config.CaptureURL, request.IdempotencyKey, "", payload)
	if err != nil {
		return digest.Receipt{}, err
	}
	if providerID == "" {
		providerID = "capture:" + request.DigestID
	}
	return digest.Receipt{ProviderID: providerID}, nil
}

func (client *Client) sendDiscord(ctx context.Context, request digest.Delivery) (digest.Receipt, error) {
	if strings.TrimSpace(client.config.DiscordURL) == "" {
		return digest.Receipt{}, &PermanentError{err: errors.New("Discord delivery is not configured")}
	}
	type field struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	type embed struct {
		Title       string  `json:"title"`
		Description string  `json:"description"`
		URL         string  `json:"url"`
		Fields      []field `json:"fields"`
	}
	embeds := make([]embed, 0, min(10, len(request.Payload.Items)))
	for _, item := range request.Payload.Items[:min(10, len(request.Payload.Items))] {
		embeds = append(embeds, embed{
			Title: escapeDiscord(item.Headline), Description: escapeDiscord(item.Summary),
			URL: absoluteAppURL(client.config.PublicBaseURL, item.AppPath),
			Fields: []field{
				{Name: "Why it matters", Value: escapeDiscord(item.Reason)},
				{Name: "Primary source", Value: item.SourceURL},
			},
		})
	}
	payload := struct {
		Content         string `json:"content"`
		AllowedMentions struct {
			Parse []string `json:"parse"`
		} `json:"allowed_mentions"`
		Embeds []embed `json:"embeds"`
	}{Content: escapeDiscord(request.Payload.ExecutiveSummary), Embeds: embeds}
	payload.AllowedMentions.Parse = []string{}
	providerID, err := client.postJSON(ctx, discordWaitURL(client.config.DiscordURL), request.IdempotencyKey, "", payload)
	if err != nil {
		return digest.Receipt{}, err
	}
	if providerID == "" {
		providerID = "discord:accepted"
	}
	return digest.Receipt{ProviderID: providerID}, nil
}

func (client *Client) sendEmail(ctx context.Context, request digest.Delivery) (digest.Receipt, error) {
	if client.config.ResendAPIKey == "" || client.config.EmailFrom == "" || client.config.EmailTo == "" || client.config.ResendURL == "" {
		return digest.Receipt{}, &PermanentError{err: errors.New("email delivery is not configured")}
	}
	var textBody strings.Builder
	var htmlBody strings.Builder
	textBody.WriteString(request.Payload.Title + "\n\n" + request.Payload.ExecutiveSummary + "\n")
	htmlBody.WriteString("<!doctype html><html lang=\"en\"><body><main><h1>" + html.EscapeString(request.Payload.Title) + "</h1><p>" + html.EscapeString(request.Payload.ExecutiveSummary) + "</p><ol>")
	for _, item := range request.Payload.Items {
		privateURL := absoluteAppURL(client.config.PublicBaseURL, item.AppPath)
		textBody.WriteString("\n" + item.Headline + "\n" + item.Summary + "\n" + privateURL + "\nSource: " + item.SourceURL + "\n")
		htmlBody.WriteString("<li><h2><a href=\"" + html.EscapeString(privateURL) + "\">" + html.EscapeString(item.Headline) + "</a></h2><p>" + html.EscapeString(item.Summary) + "</p><p><a href=\"" + html.EscapeString(item.SourceURL) + "\">Primary source</a></p></li>")
	}
	htmlBody.WriteString("</ol></main></body></html>")
	payload := map[string]any{
		"from": client.config.EmailFrom, "to": []string{client.config.EmailTo},
		"subject": request.Payload.Title, "text": textBody.String(), "html": htmlBody.String(),
		"headers": map[string]string{"X-Relantern-Digest-ID": request.DigestID},
	}
	providerID, err := client.postJSON(ctx, client.config.ResendURL, request.IdempotencyKey, "Bearer "+client.config.ResendAPIKey, payload)
	if err != nil {
		return digest.Receipt{}, err
	}
	if providerID == "" {
		return digest.Receipt{}, errors.New("Resend response did not include a provider ID")
	}
	return digest.Receipt{ProviderID: providerID}, nil
}

func (client *Client) postJSON(
	ctx context.Context,
	target string,
	idempotencyKey string,
	authorization string,
	payload any,
) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode delivery request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(encoded))
	if err != nil {
		return "", &PermanentError{err: fmt.Errorf("create delivery request: %w", err)}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", idempotencyKey)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("send delivery request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("read delivery response: %w", err)
	}
	if len(body) > maximumResponseBytes {
		return "", &PermanentError{err: errors.New("delivery response exceeded 64 KiB")}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		statusErr := fmt.Errorf("delivery provider returned status %d", response.StatusCode)
		if response.StatusCode >= http.StatusBadRequest && response.StatusCode < http.StatusInternalServerError &&
			response.StatusCode != http.StatusRequestTimeout && response.StatusCode != http.StatusTooEarly &&
			response.StatusCode != http.StatusTooManyRequests {
			return "", &PermanentError{err: statusErr}
		}
		return "", statusErr
	}
	var decoded struct {
		ID string `json:"id"`
	}
	if len(body) > 0 && json.Unmarshal(body, &decoded) != nil {
		return "", &PermanentError{err: errors.New("delivery provider returned malformed JSON")}
	}
	return decoded.ID, nil
}

func discordWaitURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	query := parsed.Query()
	query.Set("wait", "true")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func absoluteAppURL(baseURL string, path string) string {
	return strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(path, "/")
}

func escapeDiscord(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\", "*", "\\*", "_", "\\_", "~", "\\~", "`", "\\`", "|", "\\|", "@", "＠",
	)
	return replacer.Replace(value)
}
