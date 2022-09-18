package main

import (
	"fmt"
	"log"
	"math"
	"os"

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
	/*
		var opt spectral.PwelchOptions

		opt.NFFT = 4096
		opt.Noverlap = 1024
		opt.Window = nil
		opt.Pad = 4096
		opt.Scale_off = false

		Power, Freq := spectral.Pwelch(SoundData64, float64(rate_sound), &opt)

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
	*/

	ave_num := 5
	cut_freq := 0.443 * float64(rate_sound) / math.Sqrt(float64(ave_num)*float64(ave_num)-1)
	fmt.Println(cut_freq)
	Power_ave_array := make([]float64, len_sound-ave_num)
	Power_ave := float64(0.0)

	for i := 0; i < ave_num; i++ {
		Power_ave += SoundData64[i] * SoundData64[i] / float64(ave_num)
	}

	Power_ave_array[0] = Power_ave

	for i := 1; i < len_sound-ave_num; i++ {
		Power_ave = Power_ave - SoundData64[i]*SoundData64[i]/float64(ave_num) + SoundData64[i+ave_num]*SoundData64[i+ave_num]/float64(ave_num)
		Power_ave_array[i] = Power_ave
	}

	Power_diff := make([]float64, len_sound-ave_num-1)
	for i := 0; i < len_sound-ave_num-1; i++ {
		Power_diff[i] = (Power_ave_array[i+1] - Power_ave_array[i]) * float64(rate_sound)
		// dt := float64(1.0)/float64(rate_sound)
		// ~~/dt とは　* rate_sound と同じになる
	}

	pts := make(plotter.XYs, len_sound-ave_num-1)
	for i, val := range Power_diff {
		pts[i].X = float64(i) / float64(rate_sound)
		pts[i].Y = val
	}

	// インスタンスを生成
	p := plot.New()

	// 表示項目の設定
	p.Title.Text = "sound"
	p.X.Label.Text = "t"
	p.Y.Label.Text = "power_diff"

	err = plotutil.AddLinePoints(p, pts)
	if err != nil {
		panic(err)
	}

	/*
		p.X.Min = 0.15
		p.X.Max = 0.25
	*/

	// 描画結果を保存
	// "5*vg.Inch" の数値を変更すれば，保存する画像のサイズを調整できます．
	if err := p.Save(10*vg.Inch, 3*vg.Inch, "power_diff.png"); err != nil {
		panic(err)
	}
}
