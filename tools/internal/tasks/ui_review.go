package tasks

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"sshclient/tools/internal/project"
)

// runUIReview composes existing offline images; it never captures a window,
// launches an application or reads the user's live connection data.
func runUIReview(args []string) error {
	fs := flag.NewFlagSet("ui-review", flag.ContinueOnError)
	input := fs.String("input", "", "build/ 下的 PNG 或 SmallPen view JSON glob")
	output := fs.String("output", "build/ui-review", "build/ 下的联系表目录")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *input == "" {
		return fmt.Errorf("需要 -input glob 和可选 -output 目录")
	}
	if _, err := project.Chdir(); err != nil {
		return err
	}
	underBuild := func(path string) bool {
		base, _ := filepath.Abs("build")
		full, err := filepath.Abs(path)
		rel, relErr := filepath.Rel(base, full)
		return err == nil && relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	if !underBuild(*input) || !underBuild(*output) {
		return fmt.Errorf("输入 glob 与输出目录必须位于 build/")
	}
	paths, err := filepath.Glob(*input)
	if err != nil || len(paths) == 0 {
		return fmt.Errorf("没有匹配的评审文件：%s", *input)
	}
	type source struct {
		Index       int
		Name, Image string
	}
	var sources []source
	for _, path := range paths {
		name, imagePath := filepath.Base(path), path
		if filepath.Ext(path) == ".json" {
			var view struct {
				Output string
				Target struct{ Page string }
				Error  json.RawMessage
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(b, &view); err != nil {
				return err
			}
			if len(view.Error) != 0 || view.Output == "" {
				return fmt.Errorf("预览失败或没有 PNG：%s", path)
			}
			name, imagePath = view.Target.Page, view.Output
		}
		sources = append(sources, source{Index: len(sources) + 1, Name: name, Image: imagePath})
	}
	if err := os.MkdirAll(*output, 0700); err != nil {
		return err
	}
	const width, height, columns, rows = 400, 310, 3, 3
	for start := 0; start < len(sources); start += columns * rows {
		end := min(start+columns*rows, len(sources))
		canvas := image.NewRGBA(image.Rect(0, 0, columns*width, rows*height))
		draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.RGBA{232, 234, 239, 255}), image.Point{}, draw.Src)
		for i, entry := range sources[start:end] {
			f, err := os.Open(entry.Image)
			if err != nil {
				return err
			}
			img, err := png.Decode(f)
			closeErr := f.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			bounds := img.Bounds()
			if bounds.Empty() {
				return fmt.Errorf("空图像：%s", entry.Image)
			}
			scale := min(float64(width-16)/float64(bounds.Dx()), float64(height-36)/float64(bounds.Dy()))
			w, h := int(float64(bounds.Dx())*scale), int(float64(bounds.Dy())*scale)
			x, y := i%columns*width+(width-w)/2, i/columns*height+26
			// Contact sheets are navigation aids; the original PNG remains
			// the legible source for close visual review.
			for yy := range h {
				for xx := range w {
					canvas.Set(x+xx, y+yy, img.At(bounds.Min.X+xx*bounds.Dx()/w, bounds.Min.Y+yy*bounds.Dy()/h))
				}
			}
			reviewIndex(canvas, entry.Index, i%columns*width+8, i/columns*height+7)
		}
		path := filepath.Join(*output, fmt.Sprintf("sheet-%02d.png", start/(columns*rows)+1))
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		err = png.Encode(f, canvas)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	manifest, err := json.MarshalIndent(sources, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*output, "index.json"), append(manifest, '\n'), 0600); err != nil {
		return err
	}
	fmt.Printf("已汇总 %d 张离线预览：%s\n", len(sources), *output)
	return nil
}

func reviewIndex(dst draw.Image, index, x, y int) {
	// Seven-segment index avoids an extra font or image dependency in tools.
	segments := [10]byte{0x3f, 0x06, 0x5b, 0x4f, 0x66, 0x6d, 0x7d, 0x07, 0x7f, 0x6f}
	boxes := [7]image.Rectangle{image.Rect(1, 0, 7, 2), image.Rect(6, 1, 8, 7), image.Rect(6, 7, 8, 13), image.Rect(1, 12, 7, 14), image.Rect(0, 7, 2, 13), image.Rect(0, 1, 2, 7), image.Rect(1, 6, 7, 8)}
	ink := image.NewUniform(color.RGBA{39, 42, 52, 255})
	for i, digit := range fmt.Sprintf("%03d", index) {
		for bit, box := range boxes {
			if segments[digit-'0']&(1<<bit) != 0 {
				draw.Draw(dst, box.Add(image.Pt(x+i*11, y)), ink, image.Point{}, draw.Src)
			}
		}
	}
}
