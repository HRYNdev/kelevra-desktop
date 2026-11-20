//go:build !windows && !linux

package vinsluzhba

// Как служба и её хозяин называются в журнале этой системы.
const (
	Sistema      = "Windows"
	Dispetcher   = "диспетчер служб"
	Dispetcherom = "диспетчером служб"
)
