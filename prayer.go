package main

import (
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/wav"
	"github.com/gen2brain/iup-go/iup"
)

var (
	timingsDir   = "./"
	remindBefore = 5 * time.Minute
	latitude     = 30.983334
	longitude    = 41.016666
)

const (
	apiUrl = "https://api.aladhan.com/v1/calendar"
	method = 4
	school = 0
)

type Prayer struct {
	Name string
	Time time.Time
}

func (p Prayer) String() string {
	return fmt.Sprintf("%-7s %s", p.Name, p.Time.Format("03:04"))
}

// --------------------------------------------------
// Sort

type Prayers []Prayer

func (prayers Prayers) Len() int {
	return len(prayers)
}

func (prayers Prayers) Less(i, j int) bool {
	sortTable := "FDAMI" // First letter of prayer name
	ii := strings.IndexByte(sortTable, prayers[i].Name[0])
	ij := strings.IndexByte(sortTable, prayers[j].Name[0])

	if ii < ij {
		return true
	}
	return false
}

func (prayers Prayers) Swap(i, j int) {
	prayers[i], prayers[j] = prayers[j], prayers[i]
}

// --------------------------------------------------

func DownloadTimings(t time.Time) string {
	year, month, _ := t.Date()
	timingsPath := fmt.Sprintf("%vtimings-%v.json", timingsDir, t.Format(time.DateOnly))
	if _, err := os.Stat(timingsPath); os.IsNotExist(err) {
		requestUrl := fmt.Sprintf("%v/%v/%v?latitude=%v&longitude=%v&method=%v",
			apiUrl, year, int(month), latitude, longitude, method)

		fmt.Println("Downloading timings...")
		resp, err := http.Get(requestUrl)
		if err != nil {
			panic(err)
		}

		f, err := os.Create(timingsPath)
		defer f.Close()
		if err != nil {
			panic(err)
		}

		io.Copy(f, resp.Body)
	}
	return timingsPath
}

func FilterPrayers(pm map[string]interface{}) map[string]string {
	m := make(map[string]string, len(pm))

	for k, v := range pm {
		if k == "Fajr" || k == "Dhuhr" || k == "Asr" || k == "Maghrib" || k == "Isha" {
			// p := v.(string)
			// m[k] = p[:5]		// remove timezone suffix
			m[k] = v.(string)
		}
	}
	return m
}

func MapToPrayers(m map[string]string, t time.Time) Prayers {
	prayers := make(Prayers, 5)

	i := 0
	for k, v := range m {
		parsed, err := time.Parse("15:04 (-07)", v)
		if err != nil {
			panic(err)
		}

		// -1 because day and month default to 1
		parsed = parsed.AddDate(t.Year(), int(t.Month())-1, t.Day()-1)

		prayers[i] = Prayer{Name: k, Time: parsed}
		i++
	}

	sort.Sort(prayers)
	return prayers
}

func PrayerTimings(t time.Time) Prayers {
	timingsPath := DownloadTimings(t)
	today := t.Day()

	f, err := os.Open(timingsPath)
	defer f.Close()
	if err != nil {
		panic(err)
	}

	data, err := io.ReadAll(f)
	if err != nil {
		panic(err)
	}

	var jsonMap struct{ Data []interface{} }

	err = json.Unmarshal(data, &jsonMap)
	if err != nil {
		panic(err)
	}

	todayData := jsonMap.Data[today-1].(map[string]interface{})
	timings := FilterPrayers(todayData["timings"].(map[string]interface{}))

	return MapToPrayers(timings, t)
}

func FormatNextPrayer(p Prayer) string {
	rem := p.Time.Sub(time.Now())

	h := rem / time.Hour
	rem -= h * time.Hour
	m := rem / time.Minute
	rem -= m * time.Minute
	s := rem / time.Second

	return fmt.Sprintf("Next prayer is %s\nafter %02d:%02d:%02d", p.Name, h, m, s)
}

func NextPrayer(prayers Prayers) (Prayer, bool) {
	timingsChanged := false
	for _, v := range prayers {
		if time.Now().Before(v.Time) {
			return v, timingsChanged
		}
	}

	nextDay := time.Now().AddDate(0, 0, 1)
	newPrayerTimings := PrayerTimings(nextDay)

	copy(prayers, newPrayerTimings)

	timingsChanged = true
	return prayers[0], timingsChanged // next day Fajr
}

// --------------------------------------------------
// Gui

func guiMain(prayers Prayers) int {
	iup.Open()
	defer iup.Close()

	iup.SetGlobal("DEFAULTFONT", "Courier 15")

	list := iup.List()
	updateTimings := func() {
		for i, p := range prayers {
			iup.SetAttribute(list, fmt.Sprint(i+1), fmt.Sprint(p))
		}
	}
	updateTimings()

	listFrame := iup.Frame(list)
	iup.SetAttribute(listFrame, "TITLE", "Prayers times")

	np, _ := NextPrayer(prayers)
	nextPrayer := iup.Label(FormatNextPrayer(np))

	iup.SetAttribute(nextPrayer, "ALIGNMENT", "ACENTER:ACENTER")
	iup.SetAttribute(nextPrayer, "EXPAND", "YES")
	nextPrayerFrame := iup.Frame(nextPrayer)
	iup.SetAttribute(nextPrayerFrame, "TITLE", "Next Prayer")

	hbox := iup.Hbox(listFrame, nextPrayerFrame)
	iup.SetAttribute(hbox, "ALIGNMENT", "ACENTER")

	timer := iup.Timer()
	iup.SetAttribute(timer, "TIME", 1000) // 1000ms -> 1s
	iup.SetCallback(timer, "ACTION_CB", iup.TimerActionFunc(func(ih iup.Ihandle) int {
		np, timingsChanged := NextPrayer(prayers)
		if timingsChanged {
			updateTimings()
		}

		rem := np.Time.Sub(time.Now()).Round(time.Second)
		if rem == remindBefore {
			go PlaySound("tasbih.wav")
		}

		if rem == time.Second {
			go PlaySound("adhan.wav")
		}

		iup.SetAttribute(nextPrayer, "TITLE", FormatNextPrayer(np))
		return iup.DEFAULT
	}))
	iup.SetAttribute(timer, "RUN", "YES")

	// tray icon
	file, err := os.Open("icon.png")
	if err != nil {
		panic(err)
	}
	icon, err := png.Decode(file)
	iup.ImageFromImage(icon).SetHandle("icon")

	closeButton := iup.Button("Close")
	iup.SetAttribute(closeButton, "PADDING", "5x5")
	iup.SetCallback(closeButton, "ACTION", iup.ActionFunc(func(ih iup.Ihandle) int {
		return iup.CLOSE
	}))

	vbox := iup.Vbox(hbox, closeButton)
	vbox.SetAttributes(map[string]string{
		"ALIGNMENT": "ACENTER",
		"MARGIN":    "2x2",
	})

	dlg := iup.Dialog(vbox)
	dlg.SetAttributes(map[string]string{
		"TITLE":     "Prayer times in Arar",
		"TRAY":      "YES",
		"TRAYIMAGE": "icon",
		"TOPMOST":   "YES",
	})

	iup.SetCallback(dlg, "CLOSE_CB", iup.CloseFunc(func(ih iup.Ihandle) int {
		iup.SetAttribute(ih, "HIDETASKBAR", "YES")
		return iup.IGNORE
	}))

	iup.SetCallback(dlg, "TRAYCLICK_CB",
		iup.TrayClickFunc(func(ih iup.Ihandle, but, pressed, dclick int) int {
			if pressed == 1 {
				switch but {
				case 1:
					iup.SetAttribute(ih, "HIDETASKBAR", "NO")
				case 3:
					iup.SetAttribute(ih, "HIDETASKBAR", "YES")
				}
			}
			return iup.DEFAULT
		}))

	iup.Show(dlg)

	return iup.MainLoop()
}

// --------------------------------------------------
// Sound
func PlaySound(wavPath string) {
	f, err := os.Open(wavPath)
	if err != nil {
		panic(err)
	}

	streamer, format, err := wav.Decode(f)
	if err != nil {
		panic(err)
	}
	defer streamer.Close()

	speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))

	done := make(chan bool)
	speaker.Play(beep.Seq(streamer, beep.Callback(func() {
		done <- true
	})))
	<-done
}

// --------------------------------------------------

func main() {
	now := time.Now()
	prayers := PrayerTimings(now)

	guiMain(prayers)
}
