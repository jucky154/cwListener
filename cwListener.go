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
	"container/ring"
	_ "embed"
	"encoding/binary"
	"github.com/gen2brain/malgo"
	"github.com/jg1vpp/winc"
	"github.com/mash/gokmeans"
	"github.com/mjibson/go-dsp/spectral"
	"github.com/moutend/go-equalizer/pkg/equalizer"
	"github.com/thoas/go-funk"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
	"unsafe"
)

const (
	CWLISTENER_NAME  = "cwListener"
	recordtime       = 0.8 //リングバッファの時間
	limit_recordtime = 3.0 //解析に回る最低限の時間
	fft_peak_delta   = 0.3
	bandpass_width   = 0.2
)

var (
	form   *winc.Form
	view   *winc.ImageView
	pane   *winc.Panel
	combo  *winc.ComboBox
	combo3 *winc.ComboBox
)

//go:embed cwtable.dat
var morse string

var cwtable = make(map[string]string)

var (
	deviceinfos      []deviceinfostruct
	availabledevices []deviceinfostruct
	thresholdmap     map[int]float64
	device           *malgo.Device
	ctx              *malgo.AllocatedContext
)

type deviceinfostruct struct {
	devicename string
	deviceid   unsafe.Pointer
	maxsample  uint32
	minsample  uint32
}

// constがないのでvarで対応
var opt = spectral.PwelchOptions{
	NFFT:      4096,
	Noverlap:  1024,
	Window:    nil,
	Pad:       4096,
	Scale_off: false,
}

type CWView struct {
	list *winc.ListView
}

var cwview CWView

type CWItem struct {
	freq1        string
	morseresult1 string
	freq2        string
	morseresult2 string
	freq3        string
	morseresult3 string
}

var cwitemarr []CWItem

func (item CWItem) Text() (text []string) {
	text = append(text, item.freq1)
	text = append(text, item.morseresult1)
	text = append(text, item.freq2)
	text = append(text, item.morseresult2)
	text = append(text, item.freq3)
	text = append(text, item.morseresult3)
	return
}

func (item CWItem) ImageIndex() int {
	return 0
}

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

func trimnullstr(str string) string {
	b := []byte(str)
	convert := string(bytes.Trim(b[:], "\x00"))
	return convert
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

func availabledevice() (deviceinfos []deviceinfostruct) {
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

	deviceinfos = make([]deviceinfostruct, 0)
	var deviceinfo deviceinfostruct

	for _, info := range infos {
		full, err := ctx.DeviceInfo(malgo.Capture, info.ID, malgo.Shared)
		if err != nil {
			DisplayToast(info.Name() + " is " + err.Error())
		} else {
			deviceinfo.devicename = info.Name()
			deviceinfo.deviceid = info.ID.Pointer()
			deviceinfo.maxsample = full.MaxSampleRate
			deviceinfo.minsample = full.MaxChannels
			deviceinfos = append(deviceinfos, deviceinfo)
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

	form.SetSize(1200, 500)

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

	combo = winc.NewComboBox(form)
	availabledevices = availabledevice()
	for i, val := range availabledevices {
		combo.InsertItem(i, trimnullstr(val.devicename))
	}
	combo.SetSelectedItem(0)
	combo.OnSelectedChange().Bind(func(e *winc.Event) {
		_ = ctx.Uninit()
		ctx.Free()
		initdevice()
	})

	combo3 = winc.NewComboBox(form)
	thresholdmap = make(map[int]float64)
	for i := 0; i < 8; i++ {
		combo3.InsertItem(i, "閾値レベル"+strconv.Itoa(i+1))
		thresholdmap[i] = float64(i+1) * float64(0.1)
	}
	combo3.SetSelectedItem(4)

	cwview.list = winc.NewListView(form)
	cwview.list.EnableEditLabels(false)
	for i := 0; i < 3; i++ {
		cwview.list.AddColumn("解析周波数", 100)
		cwview.list.AddColumn("解析結果", 200)
	}

	cwitemarr = make([]CWItem, 3)
	for i := 0; i < len(cwitemarr); i++ {
		cwitemarr[i].freq1 = "-"
		cwitemarr[i].morseresult1 = "未解析"
		cwitemarr[i].freq2 = "-"
		cwitemarr[i].morseresult2 = "未解析"
		cwitemarr[i].freq3 = "-"
		cwitemarr[i].morseresult3 = "未解析"
	}

	for _, val := range cwitemarr {
		cwview.list.AddItem(val)

	}

	dock := winc.NewSimpleDock(form)
	dock.Dock(combo, winc.Top)
	dock.Dock(combo3, winc.Top)
	dock.Dock(cwview.list, winc.Bottom)
	dock.Dock(pane, winc.Fill)

	initdevice()
	form.Show()

	form.OnClose().Bind(closeWindow)

	return
}

func closeWindow(arg *winc.Event) {
	x, y := form.Pos()
	SetINI(CWLISTENER_NAME, "x", strconv.Itoa(x))
	SetINI(CWLISTENER_NAME, "y", strconv.Itoa(y))
	_ = ctx.Uninit()
	ctx.Free()
	form.Close()
}

type XYs []XY

type XY struct {
	X, Y float64
}

type Peak_XY struct {
	X, Y int
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func decode_main(SoundData []int32, rate_sound uint32) {
	len_sound := len(SoundData)

	//無音で回ってきたら何もしない
	if funk.MaxInt32(SoundData) == 0 {
		return
	}

	souce_arr := make([]float64, len_sound)
	norm := float64(1.0) / float64(funk.MaxInt32(SoundData))
	for i, val := range SoundData {
		souce_arr[i] = float64(val) * norm
	}

	powerarr, freqarr := PeakFreq(souce_arr, rate_sound)
	fft_peak := DetectPeakFFT(float64(fft_peak_delta), powerarr)

	//ピークがないとき
	if len(fft_peak) == 0 {
		return
	}

	cwitems := CWItem{
		freq1:        "-",
		morseresult1: "none",
		freq2:        "-",
		morseresult2: "none",
		freq3:        "-",
		morseresult3: "none",
	}

	for i := 0; i < min(len(fft_peak), 3); i++ {
		fft_peak_freq := freqarr[fft_peak[i].index]
		bpf_cfg := BPF_config{
			samplerate: float64(rate_sound),
			freq:       fft_peak_freq,
			width:      float64(bandpass_width),
		}

		Signal64 := BPF(BPF(BPF(BPF(BPF(souce_arr, bpf_cfg), bpf_cfg), bpf_cfg), bpf_cfg), bpf_cfg)

		ave_num := 6 * int(float64(rate_sound)/fft_peak_freq)

		SquaredSignal64 := make([]float64, len(Signal64))
		for i, val := range Signal64 {
			SquaredSignal64[i] = val * val
		}

		smoothed := LPF(LPF(LPF(LPF(SquaredSignal64, ave_num), ave_num), ave_num), ave_num)
		diff := OneStepDiff(smoothed)
		edge := DetectPeak(thresholdmap[combo3.SelectedItem()], diff)

		signalarr := Decode(edge)
		morsestrings := morsedecode(signalarr)
		freqstr := strconv.Itoa(int(fft_peak_freq))

		switch i + 1 {
		case 1:
			cwitems.freq1 = freqstr
			cwitems.morseresult1 = morsestrings
		case 2:
			cwitems.freq2 = freqstr
			cwitems.morseresult2 = morsestrings
		case 3:
			cwitems.freq3 = freqstr
			cwitems.morseresult3 = morsestrings
		}

		if form.Visible() {
			picupdate(smoothed, edge, rate_sound, morsestrings, i)
			if min(len(fft_peak), 3) == i+1 {
				listupdate(cwitems)
			}
		}
	}
}

func listupdate(cwitems CWItem) {
	cwitemarr[0] = cwitemarr[1]
	cwitemarr[1] = cwitemarr[2]
	cwitemarr[2] = cwitems

	cwview.list.DeleteAllItems()

	for _, val := range cwitemarr {
		cwview.list.AddItem(val)
	}
	return
}

func picupdate(smoothed []float64, edge []Peak_XY, rate_sound uint32, morsestrings string, index int) {
	pts := make(plotter.XYs, len(smoothed))

	for i, val := range smoothed {
		pts[i].X = float64(i) / float64(rate_sound)
		pts[i].Y = val
	}

	//ここからはピークの塗り潰し
	cnt := 0
	for _, val := range edge {
		if val.Y == 1 {
			cnt += 1
		}
	}

	pts_peak_min := make(plotter.XYs, len(edge)-cnt)
	pts_peak_max := make(plotter.XYs, cnt)

	cnt1 := 0
	cnt2 := 0
	for _, val := range edge {
		if val.Y == 1 {
			pts_peak_max[cnt1] = pts[val.X]
			cnt1 += 1
		} else {
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
	p.Save(10*vg.Inch, 3*vg.Inch, "smoothed"+strconv.Itoa(index)+".png")

	view.DrawImageFile("smoothed" + strconv.Itoa(index) + ".png")
	pane.Invalidate(true)

	return
}

func samplingrate(maxsample uint32, minsample uint32) (sample uint32) {
	sample = uint32(44100)
	if int(maxsample) < 44100 {
		sample = maxsample
	}
	if int(minsample) > 44100 {
		sample = minsample
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
				switch gokmeans.Nearest(gokmeans.Node{length_onoff[i]}, centroids) {
				case long_index:
					signalarr += " "
				case short_index:
					signalarr = signalarr
				}
			}
		}
		return signalarr
	} else {
		interval = funk.MinFloat64(length_onoff)
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

func PeakFreq(signal []float64, sampling_freq uint32) ([]float64, []float64) {
	Power, Freq := spectral.Pwelch(signal, float64(sampling_freq), &opt)

	peakPower := 0.0
	powerarr := make([]float64, 0)
	freqarr := make([]float64, 0)
	for i, val := range Freq {
		if val > 200 && val < 2000 {
			powerarr = append(powerarr, Power[i])
			freqarr = append(freqarr, val)
			if Power[i] > peakPower {
				peakPower = Power[i]
			}
		}
	}

	return powerarr, freqarr
}

type BPF_config struct {
	samplerate float64
	freq       float64
	width      float64
}

func BPF(input []float64, bpf_cfg BPF_config) []float64 {
	output := make([]float64, len(input))
	bpf := equalizer.NewBandPass(bpf_cfg.samplerate, bpf_cfg.freq, bpf_cfg.width)

	for i, val := range input {
		output[i] = bpf.Apply(val)
	}

	return output
}

type PeakFFT struct {
	index int
	power float64
}

func DetectPeakFFT(threshold float64, y []float64) (result []PeakFFT) {
	peak_value := funk.MaxFloat64(y)
	delta := peak_value * threshold

	mn := peak_value * float64(2.0)
	mx := float64(-1)
	var mxpos int
	result = make([]PeakFFT, 0)
	var buf PeakFFT

	lookformax := true

	for i, this := range y {
		if this > mx {
			mx = this
			mxpos = i
		}

		if this < mn {
			mn = this
		}

		if lookformax {
			if this < mx-delta {
				buf.index = mxpos
				buf.power = mx
				result = append(result, buf)
				mn = this
				lookformax = false
			}
		} else {
			if this > mn+delta {
				mx = this
				mxpos = i
				lookformax = true
			}
		}
	}

	sort.Slice(result, func(i, j int) bool { return result[i].power > result[j].power })

	return
}

func initdevice() {
	var err error
	machinenum := combo.SelectedItem()
	maxsample := availabledevices[machinenum].maxsample
	minsample := availabledevices[machinenum].minsample
	rate_sound := samplingrate(maxsample, minsample)

	ctx, err = malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		DisplayToast(message)
		return
	})

	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	if err != nil {
		DisplayModal("機器の初期化中に問題が発生しました。音声機器の接続を確認し、プラグインウィンドウを開きなおしてください")
		DisplayToast(err.Error())
		return

	}

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Duplex)
	deviceConfig.Capture.Format = malgo.FormatS32
	deviceConfig.Capture.Channels = 1
	deviceConfig.Playback.Format = malgo.FormatS32
	deviceConfig.Playback.Channels = 1
	deviceConfig.SampleRate = rate_sound
	deviceConfig.Capture.DeviceID = availabledevices[machinenum].deviceid
	deviceConfig.Alsa.NoMMap = 1
	deviceConfig.PeriodSizeInMilliseconds = uint32(float64(recordtime) * float64(1000))

	pCapturedSamples := make([]int32, 0)

	length_ring := int(float64(rate_sound) * float64(recordtime))
	length_limit := int(float64(rate_sound) * float64(limit_recordtime))
	ringbuffer := ring.New(length_ring)
	buffer_int32 := make([]int32, length_ring)
	for i := 0; i < length_ring; i++ {
		ringbuffer.Value = float64(0)
		ringbuffer = ringbuffer.Next()
	}

	onRecvFrames := func(pSample2, pSample []byte, framecount uint32) {
		Signalint := make([]int32, framecount)
		buffer := bytes.NewReader(pSample)
		binary.Read(buffer, binary.LittleEndian, &Signalint)
		for _, val := range Signalint {
			ringbuffer.Value = float64(val)
			ringbuffer = ringbuffer.Next()
		}

		if IsSilent(ringbuffer, rate_sound) {
			// 録音データが入っているかどうかのif
			if len(pCapturedSamples) > length_limit {
				go decode_main(pCapturedSamples, rate_sound)
			}
			pCapturedSamples = make([]int32, 0)
			for i := 0; i < length_ring; i++ {
				buffer_int32[i] = int32(ringbuffer.Value.(float64))
				ringbuffer = ringbuffer.Next()
			}
			pCapturedSamples = append(pCapturedSamples, buffer_int32...)
		} else {
			pCapturedSamples = append(pCapturedSamples, Signalint...)
		}
	}

	captureCallbacks := malgo.DeviceCallbacks{
		Data: onRecvFrames,
	}
	device, err = malgo.InitDevice(ctx.Context, deviceConfig, captureCallbacks)
	if err != nil {
		DisplayModal("機器の初期化中に問題が発生しました。音声機器の接続を確認し、プラグインウィンドウを開きなおしてください")
		DisplayToast(err.Error())
		return
	}

	err = device.Start()
	if err != nil {
		DisplayModal("機器のスタート時に問題が発生しました。音声機器の接続を確認し、プラグインウィンドウを開きなおしてください")
		DisplayToast(err.Error())
		return
	}
}

func IsSilent(ringbuffer *ring.Ring, sampling_freq uint32) bool {
	len_buffer := ringbuffer.Len()
	buffer_float64 := make([]float64, len_buffer)
	for i := 0; i < len_buffer; i++ {
		//取り出すときにぐるっと一周してしまえば問題ない
		buffer_float64[i] = ringbuffer.Value.(float64)
		ringbuffer = ringbuffer.Next()
	}

	Power, Freq := spectral.Pwelch(buffer_float64, float64(sampling_freq), &opt)

	mean := 0.0
	cnt := 0
	peakPower := 0.0
	for i, val := range Freq {
		if val > 200 && val < 2000 {
			mean += Power[i]
			cnt += 1
			if Power[i] > peakPower {
				peakPower = Power[i]
			}
		}
	}

	mean = mean / float64(cnt)

	switch {
	case peakPower/mean < 10.0:
		return true
	case peakPower == 0:
		return true
	default:
		return false
	}
}
