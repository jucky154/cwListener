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

/*
This software includes the work that is distributed in the Apache License 2.0.
"github.com/mash/gokmeans"
*/

package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/binary"
	"github.com/gen2brain/malgo"
	"github.com/jg1vpp/winc"
	"github.com/mash/gokmeans"
	"github.com/mjibson/go-dsp/spectral"
	"github.com/thoas/go-funk"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

const (
	CWLISTENER_NAME = "cwListener"
	CWLISTENER_MENU = "MainForm.MainMenu.CwListenerMenu"
)

var (
	form   *winc.Form
	view   *winc.ImageView
	pane   *winc.Panel
	combo  *winc.ComboBox
	combo2 *winc.ComboBox
	combo3 *winc.ComboBox
)

//go:embed cwtable.dat
var morse string

var cwtable = make(map[string]string)

var (
	devicemap    map[int]unsafe.Pointer
	maxsamplemap map[int]uint32
	minsamplemap map[int]uint32
	thresholdmap map[int]float64
)

func init() {
	OnLaunchEvent = onLaunchEvent
	winc.DllName = CWLISTENER_NAME
}

func onLaunchEvent() {
	makecwtable()
	RunDelphi(`PluginMenu.Add(op.Put(MainMenu.CreateMenuItem(), "Name", "PlugincwListenerWindow"))`)
	RunDelphi(`op.Put(MainMenu.FindComponent("PlugincwListenerWindow"), "Caption", "モールス解析")`)
	HandleButton("MainForm.MainMenu.PlugincwListenerWindow", func(num int) {
		createWindow()
	})
}

func makecwtable() {
	reader := strings.NewReader(morse)
	stream := bufio.NewScanner(reader)
	for stream.Scan() {
		val := stream.Text()
		cwtable[val[1:]] = val[0:1]
	}
	cwtable[";"] = " "
	cwtable[""] = " "
}

func availabledevice() (resultid []int, resultstring []string) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		DisplayToast(message)
	})

	if err != nil {
		DisplayToast(err.Error())
	}

	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		DisplayToast(err.Error())
	}

	devicemap = make(map[int]unsafe.Pointer)
	maxsamplemap = make(map[int]uint32)
	minsamplemap = make(map[int]uint32)
	resultid = make([]int, 0)
	resultstring = make([]string, 0)

	for i, info := range infos {
		full, err := ctx.DeviceInfo(malgo.Capture, info.ID, malgo.Shared)
		if err != nil {
			DisplayToast(info.Name() + " is " + err.Error())
		} else {
			devicemap[i] = info.ID.Pointer()
			maxsamplemap[i] = full.MaxSampleRate
			minsamplemap[i] = full.MaxChannels
			resultid = append(resultid, i)
			resultstring = append(resultstring, info.Name())
		}
	}
	return
}

func createWindow() {
	form = winc.NewForm(nil)
	form.SetText("CW LISTENER")
	icon, err := winc.ExtractIcon("zlog.exe", 0)
	if err == nil {
		form.SetIcon(0, icon)
	}

	form.SetSize(1200, 300)

	form.EnableSizable(false)
	form.EnableMaxButton(false)

	x, _ := strconv.Atoi(GetINI(CWLISTENER_NAME, "x"))
	y, _ := strconv.Atoi(GetINI(CWLISTENER_NAME, "y"))
	if x <= 0 || y <= 0 {
		form.Center()
	} else {
		form.SetPos(x, y)
	}
	pane = winc.NewPanel(form)
	view = winc.NewImageView(pane)
	form.OnClose().Bind(closeWindow)

	combo = winc.NewComboBox(form)
	deviceid, devicename := availabledevice()
	for i, val := range deviceid {
		combo.InsertItem(val, devicename[i])
	}
	combo.SetSelectedItem(deviceid[0])
	combolabel := winc.NewLabel(combo)
	combolabel.SetText("入力装置")

	combo2 = winc.NewComboBox(form)
	for i := 5; i < 21; i++ {
		combo2.InsertItem(i, strconv.Itoa(i))
	}
	combo2.SetSelectedItem(10)
	combo2label := winc.NewLabel(combo2)
	combo2label.SetText("録音時間")

	combo3 = winc.NewComboBox(form)
	thresholdmap = make(map[int]float64)
	for i := 1; i < 9; i++ {
		combo3.InsertItem(i, strconv.Itoa(i))
		thresholdmap[i] = float64(i)*float64(0.1)
	}
	combo3.SetSelectedItem(5)
	combo3label := winc.NewLabel(combo3)
	combo3label.SetText("閾値レベル")

	btn := winc.NewPushButton(form)
	btn.SetText("録音&解析")
	btn.OnClick().Bind(func(e *winc.Event) {
		update()
	})

	dock := winc.NewSimpleDock(form)
	dock.Dock(pane, winc.Fill)
	dock.Dock(combo, winc.Top)
	dock.Dock(combo2, winc.Top)
	dock.Dock(combo3, winc.Top)
	dock.Dock(btn, winc.Top)
	 
	form.Show()
	return
}

func closeWindow(arg *winc.Event) {
	x, y := form.Pos()
	SetINI(CWLISTENER_NAME, "x", strconv.Itoa(x))
	SetINI(CWLISTENER_NAME, "y", strconv.Itoa(y))
	form.Close()
}

type XYs []XY

type XY struct {
	X, Y float64
}

type Peak_XY struct {
	X, Y int
}

func update() {
	SoundData := record()
	len_sound := len(SoundData)
	rate_sound := samplingrate(combo.SelectedItem())

	Signal64 := make([]float64, len_sound)
	SquaredSignal64 := make([]float64, len_sound)
	norm := float64(1.0) / float64(funk.MaxInt32(SoundData))
	for i, val := range SoundData {
		Signal64[i] = float64(val) * norm
		SquaredSignal64[i] = float64(val) * float64(val) * norm * norm
	}

	ave_num := 6 * int(float64(rate_sound)/PeakFreq(Signal64, rate_sound))

	smoothed := LPF(LPF(LPF(LPF(SquaredSignal64, ave_num), ave_num), ave_num), ave_num)
	diff := OneStepDiff(smoothed)
	edge := DetectPeak(thresholdmap[combo3.SelectedItem()], diff)

	signalarr := Decode(edge)
	morsestrings := morsedecode(signalarr)

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

	//ここからはピークの塗り潰し
	cnt := 0
	for _, val := range edge {
		if val.Y == 1 {
			cnt += 1
		}
	}

	pts_peak_diff_min := make(plotter.XYs, len(edge)-cnt)
	pts_peak_diff_max := make(plotter.XYs, cnt)
	pts_peak_min := make(plotter.XYs, len(edge)-cnt)
	pts_peak_max := make(plotter.XYs, cnt)

	cnt1 := 0
	cnt2 := 0
	for _, val := range edge {
		if val.Y == 1 {
			pts_peak_diff_max[cnt1] = pts_diff[val.X]
			pts_peak_max[cnt1] = pts[val.X]
			cnt1 += 1
		} else {
			pts_peak_diff_min[cnt2] = pts_diff[val.X]
			pts_peak_min[cnt2] = pts[val.X]
			cnt2 += 1
		}
	}

	p := plot.New()

	p.Title.Text = morsestrings
	p.X.Label.Text = "t"
	p.Y.Label.Text = "power"

	plotutil.AddLines(p, pts)
	p1, _ := plotter.NewScatter(pts_peak_max)
	p1.GlyphStyle.Color = color.RGBA{R: 255, B: 128, A: 55} // 緑
	p2, _ := plotter.NewScatter(pts_peak_min)
	p2.GlyphStyle.Color = color.RGBA{R: 155, B: 128, A: 255} // 紫
	p.Add(p1)
	p.Add(p2)
	p.Save(10*vg.Inch, 3*vg.Inch, "smoothed.png")

	view.DrawImageFile("smoothed.png")
	pane.Invalidate(true)
}

func samplingrate(itemid int)(sample uint32) {
	sample = uint32(44100)
	if maxsamplemap[itemid] < uint32(44100) {
		sample = maxsamplemap[itemid]
	} 
	if minsamplemap[itemid] > uint32(44100){
		sample = minsamplemap[itemid]
	} 
	return
}

func Decode(signal []Peak_XY) string {
	length_on_kmnode := make([]gokmeans.Node, 0)

	//ピークはアルゴリズム的に極大スタート, 極大と極小は交互にくる, よって偶数番目にon（音あり）/奇数番目にoff（音無）が入る
	length_onoff := make([]float64, len(signal)-1)
	for i := 0; i < len(signal)-1; i++ {
		length := float64(signal[i+1].X - signal[i].X)
		length_onoff[i] = length

		//signalが1→-1になるのがonの状態なので,signal.Yが-1のときon　そのときを抽出してk-meansのノードを作る
		if signal[i+1].Y == -1 {
			length_on_kmnode = append(length_on_kmnode, gokmeans.Node{length})
		}
	}

	var interval float64
	var long_index int
	var short_index int

	if success, centroids := gokmeans.Train(length_on_kmnode, 2, 50); success {
		switch {
		case centroids[1][0] > centroids[0][0]:
			long_index = 1
			short_index = 0
			interval = centroids[0][0]
		case centroids[1][0] < centroids[0][0]:
			long_index = 0
			short_index = 1
			interval = centroids[1][0]
		}

		signalarr := ""
		node_cnt := 0

		for i := 0; i < len(signal)-1; i++ {
			//音ありの時
			if signal[i+1].Y == -1 {
				switch gokmeans.Nearest(length_on_kmnode[node_cnt], centroids) {
				case long_index:
					signalarr += "_"
				case short_index:
					signalarr += "."
				}
				node_cnt += 1
			} else {
				//音無の時
				span := int(math.Round(length_onoff[i] / interval))
				if span == 3 {
					signalarr += " "
				} else if span > 3 {
					signalarr += " ; "
				}
			}
		}
		return signalarr
	} else {
		interval = funk.MaxFloat64(length_onoff)
		return Decode_normal(signal, interval)
	}
}

func Decode_normal(signal []Peak_XY, interval float64) string {
	prev_up := 0
	prev_dn := 0
	signalarr := ""

	for _, val := range signal {
		if val.Y == 1.0 {
			prev_up = val.X
			span := int(math.Round(float64(val.X-prev_dn) / interval))
			if span == 3 {
				signalarr += " "
			} else if span > 3 {
				signalarr += " ; "
			}
		} else if val.Y == -1.0 {
			prev_dn = val.X
			span := int(math.Round(float64(val.X-prev_up) / interval))
			if span == 1 {
				signalarr += "."
			} else if span >= 2 {
				signalarr += "_"
			}
		}
	}
	return signalarr
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

func DetectPeak(threshold float64, y []float64) (result []Peak_XY) {
	peak_value := PeakValue(y)
	delta := peak_value * threshold

	mn := float64(10000)
	mx := float64(-10000)
	var mnpos int
	var mxpos int
	result = make([]Peak_XY, 0)
	var buf Peak_XY

	lookformax := true

	for i, this := range y {
		if this > mx {
			mx = this
			mxpos = i
		}

		if this < mn {
			mn = this
			mnpos = i
		}

		if lookformax {
			if this < mx-delta {
				buf.X = mxpos
				buf.Y = 1
				result = append(result, buf)
				mn = this
				mnpos = i
				lookformax = false
			}
		} else {
			if this > mn+delta {
				buf.X = mnpos
				buf.Y = -1
				result = append(result, buf)
				mx = this
				mxpos = i
				lookformax = true
			}
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

func record() []int32 {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		DisplayToast(message)
	})
	if err != nil {
		DisplayToast(err.Error())
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
	deviceConfig.SampleRate = samplingrate(combo.SelectedItem())
	deviceConfig.Capture.DeviceID = devicemap[combo.SelectedItem()]
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

	captureCallbacks := malgo.DeviceCallbacks{
		Data: onRecvFrames,
	}
	device, err := malgo.InitDevice(ctx.Context, deviceConfig, captureCallbacks)
	if err != nil {
		DisplayToast(err.Error())
	}

	err = device.Start()
	if err != nil {
		DisplayToast(err.Error())
	}

	time.Sleep(time.Second * time.Duration(combo2.SelectedItem()))

	device.Uninit()

	Signalint := make([]int32, len(pCapturedSamples)/4)
	buffer := bytes.NewReader(pCapturedSamples)
	binary.Read(buffer, binary.LittleEndian, &Signalint)

	return Signalint
}
