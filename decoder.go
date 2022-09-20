/*
MIT License
Copyright (c) 2022 JA1ZLO
Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:
The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.
THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package main

import (
       "bufio"
	_ "embed"
	"strings"
	"fmt"
	"github.com/mjibson/go-dsp/spectral"
	"bytes"
	"encoding/binary"
	"github.com/gen2brain/malgo"
	"github.com/thoas/go-funk"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
	"math"
	"os"
	"time"
)

type XYs []XY

type XY struct {
	X, Y float64
}

//go:embed cwtable.dat
var morse string

var cwtable = make(map[string]string)

var samplingrate = 44100

func Decode(signal []float64, interval float64) {
	prev_up := 0
	prev_dn := 0
	signalarr := ""

	for i, val := range signal {
		if val == 1.0 {
			prev_up = i
			span := int(math.Round(float64(i-prev_dn) / interval))
			if span == 3 {
				fmt.Print(" ")
				signalarr += " "
			} else if span > 3 {
				fmt.Println()
				signalarr += " ; "
			}
		} else if val == -1.0 {
			prev_dn = i
			span := int(math.Round(float64(i-prev_up) / interval))
			if span == 1 {
				fmt.Print(".")
				signalarr += "."
			} else if span >= 2 {
				fmt.Print("_")
				signalarr += "_"
			}
		}
	}
	fmt.Println()
	fmt.Println(morsedecode(signalarr))
}

func morsedecode(signalarr string) string {
     	reader := strings.NewReader(morse)
	stream := bufio.NewScanner(reader)
	for stream.Scan() {
		val := stream.Text()
		cwtable[val[1:]] = val[0:1]
	}
	cwtable[";"] = " "
	cwtable[""] = " "
	
	cwarr := strings.Split(signalarr, " ")
	cwstrarr := ""

	for _, cwsignal := range cwarr {
		cwstr, cwok := cwtable[cwsignal]
		if cwok {
			cwstrarr = cwstrarr + cwstr
		} else {
			cwstrarr = cwstrarr + "_?_"
		}
	}
	return cwstrarr
}

func PeakValue(source []float64) float64 {
	peak := 0.0

	for _, val := range source {
		if val > peak {
			peak = val
		} else if val < -peak {
			peak = -val
		}
	}

	return peak
}

func DetectEdges(threshold float64, source []float64) (result []float64, interval int) {
	peak_value := PeakValue(source)
	fmt.Println(peak_value)
	threshold = peak_value * threshold

	hold := 0
	count_start := 0
	interval = len(source)
	result = make([]float64, len(source))

	for i, val := range source {
		if val > threshold && hold == 0 {
			result[i] = 1.0
			hold = 1
			if i-count_start < interval {
				interval = i - count_start
			}
			count_start = i
		} else if val < -threshold && hold == 0 {
			result[i] = -1.0
			hold = -1
			if i-count_start < interval {
				interval = i - count_start
			}
			count_start = i
		} else if -threshold < val && val < threshold {
			hold = 0
		}
	}

	return
}

func OneStepDiff(source []float64) (result []float64) {
	result = make([]float64, len(source))

	for i, val := range source[1:] {
		result[i] = val - source[i]
	}

	return
}

func LPF(source []float64, n int) (result []float64) {
	result = make([]float64, len(source)-n)
	ave := float64(0.0)

	for i := 0; i < n; i++ {
		ave += source[i] / float64(n)
	}

	result[0] = ave

	for i := 1; i < len(result); i++ {
		neg := source[i+0] / float64(n)
		pos := source[i+n] / float64(n)
		ave = ave - neg + pos
		result[i] = ave
	}

	return
}

func PeakFreq(signal []float64, sampling_freq uint32) float64 {
	var opt spectral.PwelchOptions

	opt.NFFT = 4096
	opt.Noverlap = 1024
	opt.Window = nil
	opt.Pad = 4096
	opt.Scale_off = false

	Power, Freq := spectral.Pwelch(signal, float64(sampling_freq), &opt)

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

	return peakFreq
}

func record() []int32{
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		fmt.Printf("LOG <%v>\n", message)
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Duplex)
	deviceConfig.Capture.Format = malgo.FormatS32
	deviceConfig.Capture.Channels = 1
	deviceConfig.Playback.Format = malgo.FormatS32
	deviceConfig.Playback.Channels = 1
	deviceConfig.SampleRate = uint32(samplingrate)
	deviceConfig.Alsa.NoMMap = 1

	var capturedSampleCount uint32
	pCapturedSamples := make([]byte, 0)

	sizeInBytes := uint32(malgo.SampleSizeInBytes(deviceConfig.Capture.Format))
	onRecvFrames := func(pSample2, pSample []byte, framecount uint32) {

		sampleCount := framecount * deviceConfig.Capture.Channels * sizeInBytes

		newCapturedSampleCount := capturedSampleCount + sampleCount

		pCapturedSamples = append(pCapturedSamples, pSample...)

		capturedSampleCount = newCapturedSampleCount

	}

	fmt.Println("Recording...")
	captureCallbacks := malgo.DeviceCallbacks{
		Data: onRecvFrames,
	}
	device, err := malgo.InitDevice(ctx.Context, deviceConfig, captureCallbacks)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	err = device.Start()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Println("stop recording after 10 seconds...")
	time.Sleep(time.Second * 10)

	device.Uninit()

	Signalint := make([]int32, len(pCapturedSamples)/4)
	buffer := bytes.NewReader(pCapturedSamples)
	binary.Read(buffer, binary.LittleEndian, &Signalint)
	
	return Signalint
}


func main() {
	SoundData := record()
	len_sound := len(SoundData)
	rate_sound := uint32(samplingrate)

	Signal64 := make([]float64, len_sound)
	SquaredSignal64 := make([]float64, len_sound)
	norm := float64(1.0)/float64(funk.MaxInt32(SoundData))
	for i, val := range SoundData {
		Signal64[i] = float64(val) *  norm
		SquaredSignal64[i] = float64(val) * float64(val) * norm * norm
	}

	ave_num :=  6 * int(float64(rate_sound)/PeakFreq(Signal64, rate_sound))
	cut_freq := 0.443 * float64(rate_sound) / math.Sqrt(float64(ave_num)*float64(ave_num)-1)
	fmt.Println("tone freq", PeakFreq(Signal64, rate_sound))
	fmt.Println("cut_off", cut_freq)

	smoothed := LPF(LPF(LPF(LPF(SquaredSignal64, ave_num), ave_num), ave_num), ave_num)
	diff := OneStepDiff(smoothed)
	edge, interval := DetectEdges(0.2, diff)

	pts := make(plotter.XYs, len(smoothed))
	pts_diff := make(plotter.XYs, len(diff))

	for i, val := range smoothed {
		pts[i].X = float64(i) / float64(rate_sound)
		pts[i].Y = val
	}

	for i, val := range diff {
		pts_diff[i].X = float64(i) / float64(rate_sound)
		pts_diff[i].Y = val
	}

	p := plot.New()

	p.Title.Text = "signal power"
	p.X.Label.Text = "t"
	p.Y.Label.Text = "power"

	plotutil.AddLines(p, pts)
	p.Save(10*vg.Inch, 3*vg.Inch, "smoothed.png")

	p = plot.New()

	p.Title.Text = "signal power diff"
	p.X.Label.Text = "t"
	p.Y.Label.Text = "power diff"

	plotutil.AddLines(p, pts_diff)
	p.Save(10*vg.Inch, 3*vg.Inch, "diff.png")

	Decode(edge, float64(interval))
}