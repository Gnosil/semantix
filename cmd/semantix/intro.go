package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	semantixGreen = "\x1b[38;2;22;139;109m"
	shadowGreen   = "\x1b[38;2;11;72;57m"
	resetColor    = "\x1b[0m"
	introVersion  = "v0.2.0"
)

var introGlyphs = map[rune][]string{
	'S': {"11111", "10000", "10000", "11111", "00001", "00001", "11111"},
	'E': {"11111", "10000", "10000", "11110", "10000", "10000", "11111"},
	'M': {"10001", "11011", "10101", "10101", "10001", "10001", "10001"},
	'A': {"01110", "10001", "10001", "11111", "10001", "10001", "10001"},
	'N': {"10001", "11001", "10101", "10101", "10011", "10001", "10001"},
	'T': {"11111", "00100", "00100", "00100", "00100", "00100", "00100"},
	'I': {"11111", "00100", "00100", "00100", "00100", "00100", "11111"},
	'X': {"10001", "10001", "01010", "00100", "01010", "10001", "10001"},
}

type introLayout struct {
	block     string
	letterGap string
}

var (
	wideIntroLayout   = introLayout{block: "██", letterGap: "     "}
	narrowIntroLayout = introLayout{block: "█", letterGap: "  "}
	plainIntroLayout  = introLayout{}
)

func runIntro(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("intro", flag.ContinueOnError)
	fs.SetOutput(stdout)
	noAnimation := fs.Bool("no-animation", false, "render the final frame without animation")
	if err := fs.Parse(args); err != nil {
		// --help is a clean exit (U19 help contract): flag prints the usage
		// to stdout (fs.SetOutput(stdout)) and returns ErrHelp.
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	effects := supportsTerminalEffects(stdout)
	color := effects && os.Getenv("NO_COLOR") == ""
	columns, rows := terminalSize(stdout)
	layout := introLayoutForSize(columns, rows)
	if *noAnimation || !effects || layout.block == "" {
		fmt.Fprint(stdout, renderIntroFrameWithLayout(1, color, layout))
		return 0
	}

	const frameCount = 56
	frames := make([]float64, frameCount)
	for frame := range frames {
		frames[frame] = float64(frame+1) / frameCount
	}
	renderIntroAnimation(stdout, frames, color, layout, 28*time.Millisecond)
	return 0
}

func renderIntroAnimation(stdout io.Writer, frames []float64, color bool, layout introLayout, delay time.Duration) {
	// Animate in the terminal's alternate screen buffer. Some terminals do not
	// implement cursor save/restore consistently, especially after a long line
	// wraps. The alternate buffer guarantees that every intermediate frame is
	// discarded at once, then only the final wordmark enters normal scrollback.
	fmt.Fprint(stdout, "\x1b[?1049h\x1b[?25l")
	for _, progress := range frames {
		fmt.Fprint(stdout, "\x1b[2J\x1b[H")
		fmt.Fprint(stdout, renderIntroFrameWithLayout(progress, color, layout))
		time.Sleep(delay)
	}
	fmt.Fprint(stdout, "\x1b[?25h\x1b[?1049l")
	fmt.Fprint(stdout, renderIntroFrameWithLayout(1, color, layout))
}

func introLayoutForSize(columns, rows int) introLayout {
	const (
		safeMargin     = 2
		minimumRows    = 13
		letters        = len("SEMANTIX")
		letterColumns  = letters * 5
		letterGaps     = letters - 1
		maximumSpacing = 6
	)
	if rows > 0 && rows < minimumRows {
		return plainIntroLayout
	}
	if columns <= 0 {
		return wideIntroLayout
	}
	available := columns - safeMargin
	for blockWidth := 2; blockWidth >= 1; blockWidth-- {
		remaining := available - letterColumns*blockWidth
		if remaining < letterGaps {
			continue
		}
		spacing := max(1, min(maximumSpacing, remaining/letterGaps)-1)
		return introLayout{
			block:     strings.Repeat("█", blockWidth),
			letterGap: strings.Repeat(" ", spacing),
		}
	}
	return plainIntroLayout
}

func introWidth(layout introLayout) int {
	const letters = len("SEMANTIX")
	return letters*5*utf8.RuneCountInString(layout.block) + (letters-1)*utf8.RuneCountInString(layout.letterGap)
}

func supportsTerminalEffects(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0 && os.Getenv("TERM") != "dumb"
}

func renderIntroFrame(progress float64, color bool) string {
	return renderIntroFrameWithLayout(progress, color, wideIntroLayout)
}

func renderIntroFrameWithLayout(progress float64, color bool, layout introLayout) string {
	const word = "SEMANTIX"
	totalPixels := len(word) * 35
	visiblePixels := int(progress * float64(totalPixels))
	var out strings.Builder

	if color {
		out.WriteString(semantixGreen)
	}
	out.WriteString("╭──────────────────────────────╮\n")
	out.WriteString("│ ")
	if color {
		out.WriteString(introStarColor(progress))
	}
	out.WriteString("✦")
	if color {
		out.WriteString(semantixGreen)
	}
	out.WriteString("  Semantix ")
	out.WriteString(introVersion)
	out.WriteString(strings.Repeat(" ", max(1, 17-len(introVersion))))
	out.WriteString("│\n")
	out.WriteString("╰──────────────────────────────╯")
	if color {
		out.WriteString(resetColor)
	}
	out.WriteString("\n\n")
	if layout.block == "" {
		if color {
			out.WriteString(semantixGreen)
		}
		out.WriteString("SEMANTIX\n")
		if color {
			out.WriteString(resetColor)
		}
		return out.String()
	}

	for row := 0; row < 7; row++ {
		for letterIndex, letter := range word {
			glyph := introGlyphs[letter]
			for column := 0; column < 5; column++ {
				// Reveal each glyph from its top-left corner to its
				// bottom-right corner, then continue into the next glyph.
				// This creates the original diagonal sweep across the word.
				pixelIndex := letterIndex*35 + row*5 + column
				on := glyph[row][column] == '1'
				visible := pixelIndex < visiblePixels
				if on && visible {
					if color && progress < 1 && pixelIndex+18 > visiblePixels {
						out.WriteString(shadowGreen)
					} else if color {
						out.WriteString(semantixGreen)
					}
					out.WriteString(layout.block)
				} else {
					out.WriteString(strings.Repeat(" ", utf8.RuneCountInString(layout.block)))
				}
			}
			if letterIndex < len(word)-1 {
				out.WriteString(layout.letterGap)
			}
		}
		if color {
			out.WriteString(resetColor)
		}
		out.WriteByte('\n')
	}
	return out.String()
}

func introStarColor(progress float64) string {
	// Two restrained pulses: one as the scan begins and one as it settles.
	const pulseWidth = 0.22
	strength := 0.0
	if progress < pulseWidth {
		strength = math.Sin(math.Pi * progress / pulseWidth)
	} else if progress > 1-pulseWidth && progress < 1 {
		strength = math.Sin(math.Pi * (progress - (1 - pulseWidth)) / pulseWidth)
	}
	base := [3]float64{22, 139, 109}
	highlight := [3]float64{105, 255, 211}
	r := int(base[0] + (highlight[0]-base[0])*strength)
	g := int(base[1] + (highlight[1]-base[1])*strength)
	b := int(base[2] + (highlight[2]-base[2])*strength)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}
