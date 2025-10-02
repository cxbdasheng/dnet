package config

// Webhook Webhook
type Webhook struct {
	WebhookEnabled     bool   `json:"webhook_enabled"`
	WebhookURL         string `json:"webhook_url"`
	WebhookHeaders     string `json:"webhook_headers"`
	WebhookRequestBody string `json:"webhook_request_body"`
}
