package identity

import (
	"fmt"
	"strings"

	"rsc.io/qr"
)

// qrQuietZone is the blank margin, in modules, scanners need around a code.
const qrQuietZone = 4

// qrSVG renders text as a QR code in a self-contained SVG: no script, no
// external reference, only one path, so the browser can show it as an image.
func qrSVG(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	side := code.Size + 2*qrQuietZone
	var path strings.Builder
	for y := range code.Size {
		for x := range code.Size {
			if code.Black(x, y) {
				fmt.Fprintf(&path, "M%d %dh1v1h-1z", x+qrQuietZone, y+qrQuietZone)
			}
		}
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`+
		`<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="%s"/></svg>`,
		side, side, side, side, path.String()), nil
}
