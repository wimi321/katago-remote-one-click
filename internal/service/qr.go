package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	qrcode "github.com/skip2/go-qrcode"
)

func PrintQR(writer io.Writer, value string) error {
	code, err := qrcode.New(value, qrcode.Medium)
	if err != nil {
		return err
	}
	bitmap := code.Bitmap()
	for y := 0; y < len(bitmap); y += 2 {
		for x := range bitmap[y] {
			top := bitmap[y][x]
			bottom := false
			if y+1 < len(bitmap) {
				bottom = bitmap[y+1][x]
			}
			switch {
			case top && bottom:
				_, _ = io.WriteString(writer, "█")
			case top:
				_, _ = io.WriteString(writer, "▀")
			case bottom:
				_, _ = io.WriteString(writer, "▄")
			default:
				_, _ = io.WriteString(writer, " ")
			}
		}
		_, _ = io.WriteString(writer, "\n")
	}
	return nil
}

func WriteQR(home, value string) (string, error) {
	path := filepath.Join(home, "state", "connection-qr.png")
	if err := qrcode.WriteFile(value, qrcode.Medium, 384, path); err != nil {
		return "", fmt.Errorf("write QR code: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", fmt.Errorf("secure QR code: %w", err)
	}
	return path, nil
}
