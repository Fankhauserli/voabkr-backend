package templates

import "embed"

// MailFS embeds the mail templates so the compiled Go binary is self-contained.
//
//go:embed mail/*
var MailFS embed.FS
