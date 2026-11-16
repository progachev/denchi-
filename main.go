package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/getlantern/systray"
	"github.com/ushineko/sanshoku"
	"github.com/ushineko/sanshoku/all"
	"github.com/ushineko/sanshoku/battery"
    "github.com/biessek/golang-ico"
    "bytes"
    "image"
    "image/color"
    "strconv"

    "golang.org/x/image/font"
    "golang.org/x/image/font/basicfont"
    "golang.org/x/image/math/fixed"
)


// generateBatteryIcon создаёт ico-изображение размером с текстом заряда батареи
func generateBatteryIcon(level int) ([]byte, error) {
    size := 16 
    img := image.NewRGBA(image.Rect(0, 0, size, size))

    // Заливаем фон
    for y := 0; y < size; y++ {
        for x := 0; x < size; x++ {
            img.Set(x, y, color.RGBA{40, 40, 40, 255})
        }
    }

    text := strconv.Itoa(level)

    face := basicfont.Face7x13
    textWidth := len(text) * face.Advance
    startX := (size - textWidth) / 2
    startY := (size+face.Height)/2 + 1

    d := &font.Drawer{
        Dst:  img,
        Src:  image.NewUniform(color.White),
        Face: face,
        Dot:  fixed.P(startX, startY),
    }
    d.DrawString(text)

    var buf bytes.Buffer
    if err := ico.Encode(&buf, img); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}

var iconData = []byte{ /* ... данные .ico файла ... */ }

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	// Устанавливаем иконку и подсказку по умолчанию
	systray.SetIcon(iconData)
	systray.SetTitle("Battery") // На Windows Title не отображается
	systray.SetTooltip("Чтение заряда наушников...")

	// Добавляем пункт меню для выхода
	mQuit := systray.AddMenuItem("Выход", "Закрыть приложение")

	// Запускаем фоновое обновление заряда
	go updateBatteryLoop()

	// Обработка нажатия на "Выход"
	go func() {
		<-mQuit.ClickedCh
		systray.Quit()
	}()
}

func onExit() {
	// Здесь можно что-то прибрать, если нужно
}

func updateBatteryLoop() {
	// Создаём контекст для операций sanshoku
	ctx := context.Background()

	// Получаем список драйверов. Нам нужны все, чтобы не пропустить Bluetooth.
	drivers := all.Drivers()

	for {
		// Пытаемся найти устройства и прочитать заряд
		found, err := sanshoku.Scan(ctx, drivers...)
		if err != nil {
			// Ошибка сканирования (например, нет прав или устройство не отвечает)
			// Просто логируем и продолжаем, чтобы не спамить в трей
			log.Printf("Scan error: %v", err)
			systray.SetTooltip("Ошибка сканирования")
			time.Sleep(1 * time.Minute)
			continue
		}

		foundBattery := false

		var batteryName string
		var batteryLevel int

		for _, c := range found {
			dev, err := c.Open(ctx)
			if err != nil {
				continue
			}

			if src, ok := dev.(battery.Source); ok {
				batteries, err := src.Batteries(ctx)
				if err == nil {
					for _, b := range batteries {
						if b.HasLevel {
							// Сохраняем значения в внешние переменные
							batteryName = b.Name
							batteryLevel = b.Level
							foundBattery = true
							break
						}
					}
				}
			}
			dev.Close()

			if foundBattery {
				break
			}
		}

		if foundBattery {
			levelInt := int(batteryLevel)
			iconBytes, err := generateBatteryIcon(levelInt)
			if err == nil {
				systray.SetIcon(iconBytes)
			}
			systray.SetTooltip(fmt.Sprintf("%s: %d%%", batteryName, levelInt))
		} else {
			systray.SetTooltip("Наушники не найдены или заряд недоступен")
		}

		time.Sleep(1 * time.Minute)
	}
}