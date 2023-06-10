/*
Copyright (C) 2023 JA1ZLO.
*/
package main

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"github.com/gen2brain/malgo"
	"github.com/tadvi/winc"
	"github.com/tadvi/winc/w32"
	"github.com/thoas/go-funk"
	"strings"
	"unsafe"
	"zylo/morse"
	"zylo/reiwa"
	"zylo/win32"
)

const (
	CWLISTENER_WINDOW = "MainForm.MainMenu.cwListenerWindow"
	WINDOW_H          = 300
	WINDOW_W          = 700
	length_list       = 20
	INTERVAL_MS       = 400
	LIFE_THRESH       = 2
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
	monitor          morse.Monitor
	prev_results     map[int]morsedata
	old_results      []string
)

type morsedata struct {
	age	int
	text	string
}

type deviceinfostruct struct {
	devicename string
	deviceid   unsafe.Pointer
}

type CWView struct {
	list *winc.ListView
}

var cwview CWView

type CWItem struct {
	analysis    string
	morseresult string
}

var cwitemarr []CWItem

type CWListItem struct {
	index int
}

func (item CWListItem) Text() (text []string) {
	if item.index < len(cwitemarr) {
		text = append(text, cwitemarr[item.index].analysis)
		text = append(text, cwitemarr[item.index].morseresult)
	}
	return
}

func (item CWListItem) ImageIndex() int {
	return item.index
}

func init() {
	reiwa.PluginName = "cwListener"
	reiwa.OnLaunchEvent = onLaunchEvent
	reiwa.OnFinishEvent = onFinishEvent
}

func onLaunchEvent() {
	reiwa.RunDelphi(runDelphi)
	reiwa.HandleButton(CWLISTENER_WINDOW, func(num int) {
		//windowが出ているときは何もしない
		if form.Visible() {
			w32.SetForegroundWindow(form.Handle())
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

func onFinishEvent() {
	closeWindow(nil)
}

func availabledevice() (deviceinfos []deviceinfostruct) {
	var err error
	ctx, err = malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		return
	})

	if err != nil {
		reiwa.DisplayToast(err.Error())
	}

	infos, err := ctx.Devices(malgo.Capture)
	if err != nil {
		reiwa.DisplayToast(err.Error())
	}

	deviceinfos = make([]deviceinfostruct, 0)

	for _, info := range infos {
		_, err := ctx.DeviceInfo(malgo.Capture, info.ID, malgo.Shared)
		if err != nil {
			reiwa.DisplayToast(info.Name() + " is " + err.Error())
		} else {
			deviceinfo := deviceinfostruct{
				devicename: info.Name(),
				deviceid:   info.ID.Pointer(),
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
		if device != nil {
			device.Uninit()
			device = nil
		}
		initdevice()
	})

	//リスト
	cwview.list = winc.NewListView(form)
	cwview.list.EnableEditLabels(false)
	cwview.list.AddColumn("解析中", 350)
	cwview.list.AddColumn("結果", 350)

	cwitemarr = make([]CWItem, length_list)
	for i := 0; i < len(cwitemarr); i++ {
		cwitemarr[i].analysis = "-"
		cwitemarr[i].morseresult = "-"
	}

	for idx, _ := range cwitemarr {
		cwview.list.AddItem(CWListItem{
			index: idx,
		})
	}

	//dock
	dock := winc.NewSimpleDock(form)
	dock.Dock(combo, winc.Top)
	dock.Dock(cwview.list, winc.Fill)

	form.OnClose().Bind(closeWindow)

	return
}

func closeWindow(arg *winc.Event) {
	defer reiwa.DisplayPanic()
	if device != nil {
		device.Uninit()
		device = nil
	}
	form.Hide()
}

func correct_string(prev_text string, latest_text string) (shown_text string) {
	for i := len(prev_text); i >= 1; i-- {
		if strings.HasPrefix(latest_text, prev_text[:i]) {
			shown_text = prev_text[:i]
			break
		}
	}

	if shown_text == "" {
		shown_text = "-"
	}

	return
}

func decode_main(signal []float64) {
	defer reiwa.DisplayPanic()

	//見えないときは何もしない
	if !form.Visible() {
		return
	}

	decode_results := monitor.Read(signal)

	//現在の列に表示するためのリストを作成
	now_results := make([]string, length_list, length_list)
	for i := 0; i < length_list; i++ {
		now_results[i] = "-"
	}
	f_int := 0

	prev_map := prev_results

	//過去に解析したものと一致するかの処理
	for f, prev_text := range prev_map {
		miss := true
		for _, m := range decode_results {
			//前回の解析において存在し、音が続いているものは前回と比較し確定した文字列を調査する
			if f == m.Freq && m.Life >= LIFE_THRESH {
				miss = false
				text := morse.CodeToText(m.Code)
				if f_int < length_list {
					now_results[f_int] =  correct_string(prev_text.text, text)
					f_int += 1
				}
			}
		}

		//前回の解析において存在するが、音が続いていないものは右に移し、mapから削除
		if result_data, ok := prev_results[f]; ok && miss {
			if result_data.text != "-" && result_data.text != "?" && result_data.age > LIFE_THRESH{
				for i := 0; i < length_list-1; i++ {
					old_results[length_list-1-i] = old_results[length_list-2-i]
				}
				old_results[0] = result_data.text
			}
			delete(prev_results, f)
		}
	}

	//次回の解析のために確定していない文字列をmapに入れる
	for _, m := range decode_results {
		if m.Life >= LIFE_THRESH {
			if _, ok := prev_results[m.Freq]; ok {
				morseage := prev_results[m.Freq].age 
				prev_results[m.Freq] = morsedata{
						age : morseage +1 ,
						text : morse.CodeToText(m.Code),
				}
			} else {
				prev_results[m.Freq] = morsedata{
						age : 0,
						text : morse.CodeToText(m.Code),
				}
			}
		}
	}

	//リストの更新
	//フォームが存在するならば表示
	if form.Visible() {
		for i := 0; i < length_list; i++ {
			cwitemarr[i].analysis = now_results[i]
			cwitemarr[i].morseresult = old_results[i]
			cwview.list.UpdateItem(cwview.list.Items()[i])
		}
	}
	return
}

func DeviceConfig(deviceID unsafe.Pointer) (cfg malgo.DeviceConfig) {
	cfg = malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatS32
	cfg.Capture.Channels = 1
	cfg.Capture.DeviceID = deviceID
	cfg.Alsa.NoMMap = 1
	cfg.PeriodSizeInMilliseconds = INTERVAL_MS
	return
}

func readSignedInt(signal []byte) (result []float64) {
	for _, b := range funk.Chunk(signal, 4).([][]byte) {
		v := binary.LittleEndian.Uint32(b)
		result = append(result, float64(int32(v)))
	}
	return
}

func initdevice() {
	machinenum := combo.SelectedItem()
	deviceConfig := DeviceConfig(availabledevices[machinenum].deviceid)

	//前の周波数を記録しておく変数
	prev_results = make(map[int]morsedata)
	old_results = make([]string, length_list, length_list)
	for i := 0; i < length_list; i++ {
		old_results[i] = "-"
	}

	onRecvFrames := func(out, in []byte, framecount uint32) {
		go decode_main(readSignedInt(in))
	}

	captureCallbacks := malgo.DeviceCallbacks{
		Data: onRecvFrames,
	}

	var err error
	device, err = malgo.InitDevice(ctx.Context, deviceConfig, captureCallbacks)
	if err != nil {
		reiwa.DisplayModal("機器の初期化中に問題が発生しました。音声機器の接続を確認し、プラグインウィンドウを開きなおしてください")
		reiwa.DisplayToast(err.Error())
		return
	}

	monitor = morse.DefaultMonitor(int(device.SampleRate()))

	err = device.Start()
	if err != nil {
		reiwa.DisplayModal("機器のスタート時に問題が発生しました。音声機器の接続を確認し、プラグインウィンドウを開きなおしてください")
		reiwa.DisplayToast(err.Error())
		return
	}
}
