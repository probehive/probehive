package v1

type PrepareWebhookSigningSecretResponse struct {
	Integration   WebhookIntegrationResponse `json:"integration"`
	SecretVersion int64                      `json:"secretVersion"`
	SigningSecret string                     `json:"signingSecret"`
}

type WebhookIntegrationVersionRequest struct {
	Version Integer `json:"version"`
}

type WebhookIntegrationStateRequest struct {
	Enabled *bool   `json:"enabled"`
	Version Integer `json:"version"`
}
