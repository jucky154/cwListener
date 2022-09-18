package main

import (
	"fmt"
	"log"
	"os"

	"github.com/mjibson/go-dsp/spectral"
	"github.com/mjibson/go-dsp/wav"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
)

type XYs []XY

type XY struct {
	X, Y float64
}

func main() {
	// ファイルのオープン
	file, err := os.Open("JA1ZLO.wav")
	if err != nil {
		log.Fatal(err)
	}

	// Wavファイルの読み込み
	w, werr := wav.New(file)
	if werr != nil {
		log.Fatal(werr)
	}

	// データを取得
	len_sound := w.Samples
	rate_sound := w.SampleRate
	SoundData, werr := w.ReadFloats(len_sound)
	if werr != nil {
		log.Fatal(werr)
	}

	// データの変換
	SoundData64 := make([]float64, len_sound)
	for i, val := range SoundData {
		SoundData64[i] = float64(val)
	}

	pts := make(plotter.XYs, len_sound)
	for i, val := range SoundData64 {
		pts[i].X = float64(i) / float64(rate_sound)
		pts[i].Y = val
	}

	// インスタンスを生成
	p := plot.New()

	// 表示項目の設定
	p.Title.Text = "sound"
	p.X.Label.Text = "t"
	p.Y.Label.Text = "power"

	err = plotutil.AddLinePoints(p, pts)
	if err != nil {
		panic(err)
	}

	// 描画結果を保存
	// "5*vg.Inch" の数値を変更すれば，保存する画像のサイズを調整できます．
	if err := p.Save(5*vg.Inch, 5*vg.Inch, "wave.png"); err != nil {
		panic(err)
	}

	var opt spectral.PwelchOptions

	opt.NFFT = 4096
	opt.Noverlap = 1024
	opt.Window = nil
	opt.Pad = 4096
	opt.Scale_off = false

	Power, Freq := spectral.Pwelch(SoundData64, float64(rate_sound), &opt)

	pts2 := make(plotter.XYs, len(Freq))
	for i, val := range Freq {
		pts2[i].X = val
		pts2[i].Y = Power[i]
	}

	p2 := plot.New()

	// 表示項目の設定
	p2.Title.Text = "fft"
	p2.X.Label.Text = "freq"
	p2.Y.Label.Text = "power"

	p2.X.Max = 2000
	p2.X.Min = 100

	err = plotutil.AddLinePoints(p2, pts2)
	if err != nil {
		panic(err)
	}

	// 描画結果を保存
	// "5*vg.Inch" の数値を変更すれば，保存する画像のサイズを調整できます．
	if err := p2.Save(5*vg.Inch, 5*vg.Inch, "fft.png"); err != nil {
		panic(err)
	}

	peakFreq := 0.0
	peakPower := 0.0
	for i, val := range Freq {
		if val > 10 && val < 3000 {
			if Power[i] > peakPower {
				peakPower = Power[i]
				peakFreq = val
			}
		}
	}
	fmt.Println(peakFreq)
}
