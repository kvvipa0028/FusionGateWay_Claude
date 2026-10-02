//go:build !darwin || !cgo

package gui

// Only the Mac's menu bar draws the cards as an image (trayimage_darwin.go);
// other trays show the text.

func trayImageShow([]trayCell, []byte) bool { return false }

func trayImageFrame([]byte) bool { return false }

func trayImageHide() {}
