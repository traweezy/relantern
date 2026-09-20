package delivery

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/traweezy/relantern/internal/alert"
)

type alertPayload struct {
	Title          string `json:"title"`
	SourceURL      string `json:"sourceUrl"`
	PackageName    string `json:"packageName"`
	Ecosystem      string `json:"ecosystem"`
	CurrentVersion string `json:"currentVersion"`
	VersionRange   string `json:"versionRange"`
	PatchedVersion string `json:"patchedVersion"`
}

func (*PermanentError) Permanent() bool { return true }

func (client *Client) SendAlert(ctx context.Context, request alert.Delivery) (alert.Receipt, error) {
	if err := validateAlertDelivery(request); err != nil {
		return alert.Receipt{}, &PermanentError{err: err}
	}
	switch client.config.Mode {
	case "disabled":
		return alert.Receipt{}, &PermanentError{err: errors.New("external delivery is disabled")}
	case "log":
		return client.sendAlertCapture(ctx, request)
	case "live":
		if request.Channel == "discord" {
			return client.sendAlertDiscord(ctx, request)
		}
		return client.sendAlertEmail(ctx, request)
	default:
		return alert.Receipt{}, &PermanentError{err: errors.New("delivery mode is invalid")}
	}
}

func validateAlertDelivery(request alert.Delivery) error {
	if (request.Channel != "discord" && request.Channel != "email") ||
		request.Attempt < 1 || strings.TrimSpace(request.AlertID) == "" ||
		strings.TrimSpace(request.IdempotencyKey) == "" ||
		len(request.AlertID) > 128 || len(request.IdempotencyKey) > 255 ||
		!utf8.ValidString(request.AlertID) || !utf8.ValidString(request.IdempotencyKey) ||
		containsControl(request.AlertID) || containsControl(request.IdempotencyKey) {
		return errors.New("alert delivery request is incomplete")
	}
	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{"title", request.Title, 500},
		{"package name", request.PackageName, 255},
		{"ecosystem", request.Ecosystem, 80},
		{"current version", request.CurrentVersion, 100},
		{"affected range", request.VersionRange, 200},
		{"patched version", request.PatchedVersion, 100},
	} {
		if strings.TrimSpace(field.value) == "" && field.name != "patched version" {
			return fmt.Errorf("alert %s is required", field.name)
		}
		if !utf8.ValidString(field.value) || utf8.RuneCountInString(field.value) > field.limit || containsControl(field.value) {
			return fmt.Errorf("alert %s is invalid", field.name)
		}
	}
	if len(request.SourceURL) > 4096 || !validAlertSourceURL(request.SourceURL) {
		return errors.New("alert source URL must be HTTPS")
	}
	return nil
}

func discordAlertTitle(title string) string {
	runes := []rune(title)
	if len(runes) > 125 {
		return escapeDiscord(string(runes[:125]) + "…")
	}
	return escapeDiscord(title)
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func validAlertSourceURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" &&
		parsed.User == nil && parsed.Opaque == "" && !containsControl(value)
}

func alertDetails(request alert.Delivery) alertPayload {
	return alertPayload{
		Title: request.Title, SourceURL: request.SourceURL, PackageName: request.PackageName,
		Ecosystem: request.Ecosystem, CurrentVersion: request.CurrentVersion,
		VersionRange: request.VersionRange, PatchedVersion: request.PatchedVersion,
	}
}

func (client *Client) sendAlertCapture(ctx context.Context, request alert.Delivery) (alert.Receipt, error) {
	payload := struct {
		Kind           string       `json:"kind"`
		AlertID        string       `json:"alertId"`
		Attempt        int          `json:"attempt"`
		Channel        string       `json:"channel"`
		IdempotencyKey string       `json:"idempotencyKey"`
		Payload        alertPayload `json:"payload"`
	}{
		Kind: "critical_alert", AlertID: request.AlertID, Attempt: request.Attempt,
		Channel: request.Channel, IdempotencyKey: request.IdempotencyKey,
		Payload: alertDetails(request),
	}
	providerID, err := client.postJSON(ctx, client.config.CaptureURL, request.IdempotencyKey, "", payload)
	if err != nil {
		return alert.Receipt{}, err
	}
	if providerID == "" {
		providerID = "capture:" + request.AlertID
	}
	return alert.Receipt{ProviderID: providerID}, nil
}

func (client *Client) sendAlertDiscord(ctx context.Context, request alert.Delivery) (alert.Receipt, error) {
	if strings.TrimSpace(client.config.DiscordURL) == "" {
		return alert.Receipt{}, &PermanentError{err: errors.New("Discord delivery is not configured")}
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
	fields := []field{
		{Name: "Package", Value: escapeDiscord(request.PackageName)},
		{Name: "Ecosystem", Value: escapeDiscord(request.Ecosystem)},
		{Name: "Installed version", Value: escapeDiscord(request.CurrentVersion)},
		{Name: "Affected versions", Value: escapeDiscord(request.VersionRange)},
	}
	if request.PatchedVersion != "" {
		fields = append(fields, field{Name: "Patched version", Value: escapeDiscord(request.PatchedVersion)})
	}
	payload := struct {
		Content         string `json:"content"`
		AllowedMentions struct {
			Parse []string `json:"parse"`
		} `json:"allowed_mentions"`
		Embeds []embed `json:"embeds"`
	}{
		Content: "Critical security alert for a watched package",
		Embeds: []embed{{
			Title:       discordAlertTitle(request.Title),
			Description: "A confirmed critical advisory affects a watched version. Review the official advisory and update the package.",
			URL:         request.SourceURL, Fields: fields,
		}},
	}
	payload.AllowedMentions.Parse = []string{}
	providerID, err := client.postJSON(ctx, discordWaitURL(client.config.DiscordURL), request.IdempotencyKey, "", payload)
	if err != nil {
		return alert.Receipt{}, err
	}
	if providerID == "" {
		providerID = "discord:accepted"
	}
	return alert.Receipt{ProviderID: providerID}, nil
}

func (client *Client) sendAlertEmail(ctx context.Context, request alert.Delivery) (alert.Receipt, error) {
	if client.config.ResendAPIKey == "" || client.config.EmailFrom == "" || client.config.EmailTo == "" || client.config.ResendURL == "" {
		return alert.Receipt{}, &PermanentError{err: errors.New("email delivery is not configured")}
	}
	var textBody strings.Builder
	fmt.Fprintf(&textBody, "Critical security alert: %s\n\nA confirmed critical advisory affects a watched package.\n\nPackage: %s\nEcosystem: %s\nInstalled version: %s\nAffected versions: %s\n", request.Title, request.PackageName, request.Ecosystem, request.CurrentVersion, request.VersionRange)
	if request.PatchedVersion != "" {
		fmt.Fprintf(&textBody, "Patched version: %s\n", request.PatchedVersion)
	}
	fmt.Fprintf(&textBody, "\nOfficial advisory: %s\n", request.SourceURL)

	escape := html.EscapeString
	var htmlBody strings.Builder
	htmlBody.WriteString("<!doctype html><html lang=\"en\" dir=\"ltr\"><head><meta charset=\"utf-8\"><title>Critical security alert</title></head><body><main lang=\"en\" dir=\"ltr\"><h1>Critical security alert</h1><p>")
	htmlBody.WriteString(escape(request.Title))
	htmlBody.WriteString("</p><p>A confirmed critical advisory affects a watched package. Review the official advisory and update the package.</p><dl>")
	for _, item := range []struct{ name, value string }{
		{"Package", request.PackageName}, {"Ecosystem", request.Ecosystem},
		{"Installed version", request.CurrentVersion}, {"Affected versions", request.VersionRange},
	} {
		fmt.Fprintf(&htmlBody, "<dt>%s</dt><dd>%s</dd>", item.name, escape(item.value))
	}
	if request.PatchedVersion != "" {
		fmt.Fprintf(&htmlBody, "<dt>Patched version</dt><dd>%s</dd>", escape(request.PatchedVersion))
	}
	fmt.Fprintf(&htmlBody, "</dl><p><a href=\"%s\">Read the official advisory</a></p></main></body></html>", escape(request.SourceURL))
	payload := map[string]any{
		"from": client.config.EmailFrom, "to": []string{client.config.EmailTo},
		"subject": "Critical security alert: " + request.Title,
		"text":    textBody.String(), "html": htmlBody.String(),
		"headers": map[string]string{"X-Relantern-Alert-ID": request.AlertID},
	}
	providerID, err := client.postJSON(ctx, client.config.ResendURL, request.IdempotencyKey, "Bearer "+client.config.ResendAPIKey, payload)
	if err != nil {
		return alert.Receipt{}, err
	}
	if providerID == "" {
		return alert.Receipt{}, errors.New("Resend response did not include a provider ID")
	}
	return alert.Receipt{ProviderID: providerID}, nil
}
