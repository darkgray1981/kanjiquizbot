package main

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"io/ioutil"
	"log"
	"math"
	"strings"

	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

var (
	fontDpi     = 72.0         // font DPI setting
	fontFile    = "meiryo.ttc" // TTF font filename
	fontHinting = "full"       // none | full
	fontSize    = 72.0         // font size in points
	fontTtf     *truetype.Font
)

// Load Font from disk
func loadFont() {

	// Read the font data
	fontBytes, err := ioutil.ReadFile(RESOURCES_FOLDER + fontFile)
	if err != nil {
		log.Fatalln("ERROR, Loading font:", err)
	}

	fontTtf, err = truetype.Parse(fontBytes)
	if err != nil {
		log.Fatalln("ERROR, Parsing font:", err)
	}
}

// Generate a PNG image reader with given string written
func GenerateImage(input string) *bytes.Buffer {

	if len(input) == 0 {
		log.Println("ERROR, Can't generate image without input")
		return nil
	}

	// Set up font hinting
	h := font.HintingNone
	switch fontHinting {
	case "full":
		h = font.HintingFull
	}

	// Pick colours
	fg, bg := image.Black, image.White

	// Set up font drawer
	d := &font.Drawer{
		Src: fg,
		Face: truetype.NewFace(fontTtf, &truetype.Options{
			Size:    fontSize,
			DPI:     fontDpi,
			Hinting: h,
		}),
	}

	// Prepare lines to be drawn
	lines := strings.Split(input, "\n")

	// Figure out image bounds
	var widest int
	for _, line := range lines {
		width := d.MeasureString(line).Round()
		if width > widest {
			widest = width
		}
	}

	lineHeight := int(math.Ceil(fontSize * fontDpi / 72 * 1.18))
	imgW := widest * 11 / 10 // 10% extra for margins
	imgH := len(lines) * lineHeight

	// Create image canvas
	rgba := image.NewRGBA(image.Rect(0, 0, imgW, imgH))

	// Draw the background and the guidelines
	draw.Draw(rgba, rgba.Bounds(), bg, image.ZP, draw.Src)

	// Attach image to font drawer
	d.Dst = rgba

	// Figure out writing position
	y := int(math.Ceil(fontSize * fontDpi / 72 * 0.94))
	x := fixed.I(imgW-widest) / 2
	for _, line := range lines {
		d.Dot = fixed.Point26_6{
			X: x,
			Y: fixed.I(y),
		}

		// Write out the text
		d.DrawString(line)

		// Advance line position
		y += lineHeight
	}

	// Encode PNG image
	var buf bytes.Buffer
	err := png.Encode(&buf, rgba)
	if err != nil {
		log.Println("ERROR, Encoding PNG with '"+input+"':", err)
		return &buf
	}

	return &buf
}
