package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log"
	"strconv"
	"time"

	"github.com/biessek/golang-ico"
	"github.com/getlantern/systray"
	"github.com/ushineko/sanshoku"
	"github.com/ushineko/sanshoku/all"
	"github.com/ushineko/sanshoku/battery"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	// iconSize — сторона квадратной иконки трея (пиксели).
	iconSize = 16

	// scanPeriod — интервал опроса заряда. Bluetooth-сканирование дорогое,
	// поэтому не стоит опрашивать чаще раза в несколько минут.
	scanPeriod = 5 * time.Minute

	// baselineOffset — поправка вертикального центрирования текста
	// для basicfont.Face7x13 на холсте iconSize x iconSize.
	baselineOffset = 1
)

// iconData — .ico-иконка по умолчанию, показывается до первого успешного опроса.
// ВАЖНО: замените на реальные байты .ico-файла. Пока заглушка — приложение
// запустится, но иконка будет пустой; при первом успешном опросе она обновится.
var iconData = []byte{}

// appCancel позволяет onExit остановить фоновые горутины.
var appCancel context.CancelFunc

// generateBatteryIconICO рисует квадрат iconSize x iconSize с числом заряда
// и кодирует результат в ICO. Возвращает байты, пригодные для systray.SetIcon.
// Уровень клампится в диапазон [0, 100].
func generateBatteryIconICO(level int) ([]byte, error) {
	if level < 0 {
		level = 0
	}
	if level > 100 {
		level = 100
	}

	img := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))

	// Заливаем фон одним вызовом — быстрее, чем img.Set в двойном цикле.
	draw.Draw(
		img,
		img.Bounds(),
		&image.Uniform{C: color.RGBA{R: 40, G: 40, B: 40, A: 255}},
		image.Point{},
		draw.Src,
	)

	text := strconv.Itoa(level)
	face := basicfont.Face7x13

	textWidth := len(text) * int(face.Advance)
	startX := (iconSize - textWidth) / 2
	// Базовая линия Face7x13 находится внизу; для вертикального центрирования
	// на холсте высотой iconSize используем (iconSize+face.Height)/2 + baselineOffset.
	startY := (iconSize+face.Height)/2 + baselineOffset

	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.White),
		Face: face,
		Dot:  fixed.P(startX, startY),
	}
	d.DrawString(text)

	var buf bytes.Buffer
	if err := ico.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode ico: %w", err)
	}
	return buf.Bytes(), nil
}

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	if len(iconData) > 0 {
		systray.SetIcon(iconData)
	}
	systray.SetTooltip("Чтение заряда наушников...")

	mQuit := systray.AddMenuItem("Выход", "Закрыть приложение")

	ctx, cancel := context.WithCancel(context.Background())
	appCancel = cancel

	go updateBatteryLoop(ctx)

	go func() {
		select {
		case <-mQuit.ClickedCh:
		case <-ctx.Done():
		}
		systray.Quit()
	}()
}

func onExit() {
	if appCancel != nil {
		appCancel()
	}
}

// updateBatteryLoop периодически сканирует устройства, читает заряд и обновляет
// иконку и tooltip. Завершается по отмене ctx.
func updateBatteryLoop(ctx context.Context) {
	drivers := all.Drivers()
	ticker := time.NewTicker(scanPeriod)
	defer ticker.Stop()

	// Первый опрос — сразу, не ждём тик.
	refreshBattery(ctx, drivers)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshBattery(ctx, drivers)
		}
	}
}

// refreshBattery делает один цикл: сканирует, находит первую батарею с уровнем,
// обновляет иконку и tooltip. Ошибки не прерывают цикл — только логируются.
func refreshBattery(ctx context.Context, drivers []sanshoku.Driver) {
	found, err := sanshoku.Scan(ctx, drivers...)
	if err != nil {
		log.Printf("scan: %v", err)
		systray.SetTooltip("Ошибка сканирования")
		return
	}

	name, level, ok := findBattery(ctx, found)
	if !ok {
		systray.SetTooltip("Наушники не найдены или заряд недоступен")
		return
	}

	iconBytes, err := generateBatteryIconICO(level)
	if err != nil {
		log.Printf("generate icon: %v", err)
		// Иконку не обновили, но tooltip обновим — это полезная информация.
	} else {
		systray.SetIcon(iconBytes)
	}

	systray.SetTooltip(fmt.Sprintf("%s: %d%%", name, level))
}

// findBattery перебирает найденные кандидаты и возвращает первую батарею
// с известным уровнем.
func findBattery(ctx context.Context, candidates []sanshoku.Candidate) (name string, level int, ok bool) {
	for _, c := range candidates {
		if name, level, ok := readOne(ctx, c); ok {
			return name, level, true
		}
	}
	return "", 0, false
}

// readOne открывает одного кандидата, читает батареи и корректно закрывает
// устройство через defer — даже если внутри произойдёт ошибка или паника.
func readOne(ctx context.Context, c sanshoku.Candidate) (name string, level int, ok bool) {
	dev, err := c.Open(ctx)
	if err != nil {
		log.Printf("open: %v", err)
		return "", 0, false
	}
	defer dev.Close()

	src, isBattery := dev.(battery.Source)
	if !isBattery {
		return "", 0, false
	}

	batteries, err := src.Batteries(ctx)
	if err != nil {
		log.Printf("batteries: %v", err)
		return "", 0, false
	}

	for _, b := range batteries {
		if b.HasLevel {
			return b.Name, int(b.Level), true
		}
	}
	return "", 0, false
}