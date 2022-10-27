/*
 Copyright (C) 2022 JA1ZLO.
*/
package main

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"github.com/gen2brain/malgo"
	"github.com/r9y9/gossp/stft"
	"github.com/r9y9/gossp/window"
	"github.com/tadvi/winc"
	"math"
	"unsafe"
	"strings"
	"zylo/morse"
	"zylo/reiwa"
	"zylo/win32"
)

const (
	CWLISTENER_WINDOW = "MainForm.MainMenu.cwListenerWindow"
	WINDOW_H          = 300
	WINDOW_W          = 700
	length_list       = 10
	recordtime        = 0.3  //リングバッファの時間[s]
	limit_recordtime  = 60.0 //解析に回せる最大の時間[s]
)

var (
	//go:embed cwListener.pas
	runDelphi string
)

var (
	form  *winc.Form
	view  *winc.ImageView
	pane  *winc.Panel
	combo *winc.ComboBox
)

var (
	availabledevices []deviceinfostruct
	device           *malgo.Device
	ctx              *malgo.AllocatedContext
)

type deviceinfostruct struct {
	devicename string
	deviceid   unsafe.Pointer
	maxsample  uint32
	minsample  uint32
}

type CWView struct {
	list *winc.ListView
}

var cwview CWView

type CWItem struct {
	status       string
	morseresult1 string
	morseresult2 string
	morseresult3 string
}

var cwitemarr []CWItem

func (item CWItem) Text() (text []string) {
	text = append(text, item.status)
	text = append(text, item.morseresult1)
	text = append(text, item.morseresult2)
	text = append(text, item.morseresult3)
	return
}

func (item CWItem) ImageIndex() int {
	return 0
}

func init() {
	reiwa.PluginName = "cwListener"
	reiwa.OnLaunchEvent = onLaunchEvent
}

func onLaunchEvent() {
	reiwa.RunDelphi(runDelphi)
	reiwa.HandleButton(CWLISTENER_WINDOW, func(num int) {
		//windowが出ているときは何もしない
		if form.Visible() {
			return
		}

		//コンボボックスの更新
		availabledevices = availabledevice()
		combo.DeleteAllItems()
		for i, val := range availabledevices {
			combo.InsertItem(i, trimnullstr(val.devicename))
		}
		combo.SetSelectedItem(0)

		//フォームを表示
		form.Show()

		//解析開始
		initdevice()
		return
	})

	createWindow()
}

func availabledevice() (deviceinfos []deviceinfostruct) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		reiwa.DisplayToast(message)
	})

	if err != nil {
		reiwa.DisplayToast(err.Error())
	}

	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		reiwa.DisplayToast(err.Error())
	}

	deviceinfos = make([]deviceinfostruct, 0)

	for _, info := range infos {
		full, err := ctx.DeviceInfo(malgo.Capture, info.ID, malgo.Shared)
		if err != nil {
			reiwa.DisplayToast(info.Name() + " is " + err.Error())
		} else {
			deviceinfo := deviceinfostruct{
				devicename: info.Name(),
				deviceid:   info.ID.Pointer(),
				maxsample:  full.MaxSampleRate,
				minsample:  full.MinSampleRate,
			}
			deviceinfos = append(deviceinfos, deviceinfo)
		}
	}
	return
}

func trimnullstr(str string) string {
	b := []byte(str)
	convert := string(bytes.Trim(b[:], "\x00"))
	return convert
}

func createWindow() {
	//フォーム作成
	form = win32.NewForm(nil)
	form.SetSize(WINDOW_W, WINDOW_H)

	//コンボボックス
	combo = winc.NewComboBox(form)
	combo.OnSelectedChange().Bind(func(e *winc.Event) {
		device.Uninit()
		ctx.Uninit()
		ctx.Free()
		initdevice()
	})

	//リスト
	cwview.list = winc.NewListView(form)
	cwview.list.EnableEditLabels(false)
	cwview.list.AddColumn("状況", 100)
	for i := 0; i < 3; i++ {
		cwview.list.AddColumn("解析結果", 200)
	}

	cwitemarr = make([]CWItem, length_list)
	for i := 0; i < len(cwitemarr); i++ {
		cwitemarr[i].status = "-"
		cwitemarr[i].morseresult1 = "-"
		cwitemarr[i].morseresult2 = "-"
		cwitemarr[i].morseresult3 = "-"
	}

	for _, val := range cwitemarr {
		cwview.list.AddItem(val)

	}

	//dock
	dock := winc.NewSimpleDock(form)
	dock.Dock(combo, winc.Top)
	dock.Dock(cwview.list, winc.Fill)

	form.OnClose().Bind(closeWindow)

	return
}

func closeWindow(arg *winc.Event) {
	device.Uninit()
	_ = ctx.Uninit()
	ctx.Free()
	form.Hide()
}

//この変数はモールス解析用（音声入力装置によって値が変わるので、下で代入する）
var decoder morse.Decoder

var monitor morse.Monitor

var before_finish bool

var prev_texts []string

func status_bool(before_finish, finish bool) (result string) {
	switch {
	case before_finish == true && finish == true :
		result = "ノイズのみ"
	case before_finish == false && finish == true :
		result = "確定"
	default :
		result = "解析中"
	}
	return
}

func correct_string(prev_text string , latest_text string, finish bool) (shown_text string) {
	for i := len(prev_text); i>= 1; i-- {
		if strings.HasPrefix(latest_text, prev_text[:i]) {
			shown_text = prev_text[:i]
			break
		}
	}

	if shown_text == ""{
		shown_text = "-"
	}

	if finish {
		shown_text = latest_text
	}
	return
}

func decode_main(signal []float64) {
	defer reiwa.DisplayPanic()

	//見えないときは何もしない
	if !form.Visible() {
		return
	}

	decode_result := monitor.Read(signal)

	finish := true
	morse_texts := make([]string, 0)
	for _, message := range decode_result {
		morse_texts = append(morse_texts, morse.CodeToText(message.Code)) 
		if !message.Finish() {
			finish = false
		}
	}

	if len(decode_result) == 0 {
		return
	}

	//まず、空の結果を最初に入れて置き、結果があるところは後で修正
	cwitems := CWItem{
		status:       status_bool(before_finish, finish),
		morseresult1: "-",
		morseresult2: "-",
		morseresult3: "-",
	}

	for i := 0; i < int(math.Min(float64(len(decode_result)), float64(3))); i++ {
		latest_text := morse_texts[i]
		switch i + 1 {
		case 1:
			cwitems.morseresult1 = correct_string(prev_texts[i], latest_text, finish)
		case 2:
			cwitems.morseresult2 = correct_string(prev_texts[i], latest_text, finish)
		case 3:
			cwitems.morseresult3 = correct_string(prev_texts[i], latest_text, finish)
		}
		prev_texts[i] = latest_text
	}

	cwitemarr[length_list-1] = cwitems

	//リストの更新
	cwview.list.DeleteAllItems()
	for _, val := range cwitemarr {
		cwview.list.AddItem(val)
	}

	switch {
	case before_finish == false && finish == true :
		before_finish = finish
		for i, val := range cwitemarr[1:] {
			cwitemarr[i] = val
		}
	default :
		before_finish = finish
	}
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

func initdevice() {
	var err error
	machinenum := combo.SelectedItem()
	maxsample := availabledevices[machinenum].maxsample
	minsample := availabledevices[machinenum].minsample
	rate_sound := samplingrate(maxsample, minsample)
	ctx, err = malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		reiwa.DisplayToast(message)
		return
	})

	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	if err != nil {
		reiwa.DisplayModal("機器の初期化中に問題が発生しました。音声機器の接続を確認し、プラグインウィンドウを開きなおしてください")
		reiwa.DisplayToast(err.Error())
		return

	}

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Duplex)
	deviceConfig.Capture.Format = malgo.FormatS32
	deviceConfig.Capture.Channels = 1
	deviceConfig.SampleRate = rate_sound
	deviceConfig.Capture.DeviceID = availabledevices[machinenum].deviceid
	deviceConfig.Alsa.NoMMap = 1
	deviceConfig.PeriodSizeInMilliseconds = uint32(1000)

	length_buffer := int(float64(rate_sound) * float64(recordtime))

	sound_buffer := make([]float64, 0)

	//decodeに必要な情報をここで入れる
	decoder = morse.Decoder{
		Thre: 0.1,
		Iter: 10,
		Bias: 10,
		STFT: &stft.STFT{
			FrameShift: int(rate_sound) / 50,
			FrameLen:   2048,
			Window:     window.CreateHanning(2048),
		},
	}

	monitor = morse.Monitor{
		MaxHold: int(rate_sound) * 60,
		Decoder: decoder,
	}

	before_finish = true 

	prev_texts = []string {"-", "-", "-"}

	buffer_cnt := 0

	onRecvFrames := func(pSample2, pSample []byte, framecount uint32) {
		Signalint := make([]int32, framecount)
		buffer := bytes.NewReader(pSample)
		binary.Read(buffer, binary.LittleEndian, &Signalint)
		for _, val := range Signalint {
			sound_buffer = append(sound_buffer, float64(val))
			buffer_cnt += 1
			if buffer_cnt == length_buffer {
				go decode_main(sound_buffer)
				sound_buffer = make([]float64, 0)
				buffer_cnt = 0
			}
		}
	}

	captureCallbacks := malgo.DeviceCallbacks{
		Data: onRecvFrames,
	}

	device, err = malgo.InitDevice(ctx.Context, deviceConfig, captureCallbacks)
	if err != nil {
		reiwa.DisplayModal("機器の初期化中に問題が発生しました。音声機器の接続を確認し、プラグインウィンドウを開きなおしてください")
		reiwa.DisplayToast(err.Error())
		return
	}

	err = device.Start()
	if err != nil {
		reiwa.DisplayModal("機器のスタート時に問題が発生しました。音声機器の接続を確認し、プラグインウィンドウを開きなおしてください")
		reiwa.DisplayToast(err.Error())
		return
	}
}
