//go:build !darwin

package syncfolder

import "context"

// requestDownloads does nothing: only macOS has iCloud Drive placeholders to ask for.
func requestDownloads(context.Context, []string) {}
