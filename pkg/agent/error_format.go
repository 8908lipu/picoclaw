package agent

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/sipeed/picoclaw/pkg/providers"
)

var (
	secretQueryPattern      = regexp.MustCompile(`(?i)(?:key|api_key|token)=([^&\s]+)`)
	secretBearerPattern     = regexp.MustCompile(`(?i)Bearer\s+([A-Za-z0-9._~+/-]{10,})`)
	secretGeminiKeyPattern  = regexp.MustCompile(`\b(AQ\.[A-Za-z0-9._-]{20,}|AIza[A-Za-z0-9._-]{25,})\b`)
	secretGenericKeyPattern = regexp.MustCompile(`\b(sk-[A-Za-z0-9._-]{15,})\b`)
)

func redactSecrets(s string) string {
	s = secretQueryPattern.ReplaceAllString(s, "key=[REDACTED]")
	s = secretBearerPattern.ReplaceAllString(s, "Bearer [REDACTED]")
	s = secretGeminiKeyPattern.ReplaceAllString(s, "[REDACTED_API_KEY]")
	s = secretGenericKeyPattern.ReplaceAllString(s, "[REDACTED_API_KEY]")
	return s
}

func formatProcessingError(err error) string {
	if err == nil {
		return ""
	}

	var exhausted *providers.FallbackExhaustedError
	if errors.As(err, &exhausted) && exhausted != nil {
		return fmt.Sprintf(
			"⚠️ All configured AI model providers are currently unavailable or rate-limited.\n\nPlease try again later or switch models using /switch.\n\nDetails:\n%s",
			redactSecrets(exhausted.Error()),
		)
	}

	if kind, ok := providers.ClassifyAuthError(err); ok {
		return fmt.Sprintf(
			"Error processing message: %s\n\nOriginal error:\n%s",
			authErrorFriendlyMessage(kind),
			redactSecrets(err.Error()),
		)
	}

	return fmt.Sprintf("Error processing message: %s", redactSecrets(err.Error()))
}

func authErrorFriendlyMessage(kind providers.AuthErrorKind) string {
	switch kind {
	case providers.AuthErrorInvalidAPIKey:
		return "Authentication failed: the API key appears to be invalid. Check the API key configured for this model or provider."
	case providers.AuthErrorMissingAPIKey:
		return "Authentication failed: no API key is configured for this model or provider. Add an API key in the model settings or config."
	case providers.AuthErrorExpiredToken:
		return "Authentication failed: the saved login or token appears to be expired. Re-authenticate the provider."
	default:
		return "Authentication failed: check the API key, token, OAuth login, or provider permissions for this model."
	}
}
