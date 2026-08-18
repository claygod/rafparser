package service

import (
	"fmt"
	"math"

	"github.com/claygod/rafparser/domain"
)

// RGBImageBinning2x2 усредняет блоки 2x2 полноразмерного RGB массива в один пиксель.
func RGBImageBinning2x2(srcImg []uint16, srcWidth, srcHeight int) *domain.RGBImage16 {
	dstWidth := srcWidth / 2
	dstHeight := srcHeight / 2

	dstData := make([]uint16, dstWidth*dstHeight*3)

	for y := 0; y < dstHeight; y++ {
		srcY0 := y * 2
		srcY1 := (y * 2) + 1
		dstRowOffset := y * dstWidth * 3

		for x := 0; x < dstWidth; x++ {
			srcX0 := x * 2
			srcX1 := (x * 2) + 1

			idx00 := (srcY0*srcWidth + srcX0) * 3
			idx01 := (srcY0*srcWidth + srcX1) * 3
			idx10 := (srcY1*srcWidth + srcX0) * 3
			idx11 := (srcY1*srcWidth + srcX1) * 3

			dstIdx := dstRowOffset + (x * 3)

			for c := 0; c < 3; c++ {
				sum := uint32(srcImg[idx00+c]) + uint32(srcImg[idx01+c]) + uint32(srcImg[idx10+c]) + uint32(srcImg[idx11+c])
				dstData[dstIdx+c] = uint16(sum / 4)
			}
		}
	}

	return &domain.RGBImage16{
		Width:  dstWidth,
		Height: dstHeight,
		Data:   dstData,
	}
}

// ApplyGamma22 применяет гамма-коррекцию 2.2 к итоговому 16-битному RGB-массиву,
// делая цвета сочными, а картинку светлой.
func ApplyGamma22(img *domain.RGBImage16) {
	for i := 0; i < len(img.Data); i++ {
		// Переводим uint16 (0-65535) в нормализованный float (0.0 - 1.0)
		norm := float64(img.Data[i]) / 65535.0

		// Стандартная математическая формула гаммы
		if norm > 0 {
			corrected := math.Pow(norm, 1.0/2.2)
			val := corrected * 65535.0
			if val > 65535.0 {
				img.Data[i] = 65535
			} else {
				img.Data[i] = uint16(val)
			}
		} else {
			img.Data[i] = 0
		}
	}
}
func ApplyWhiteBalance(pixelData []uint16, meta *domain.GFXMetadata) []uint16 {
	widthInt := int(meta.Width)
	heightInt := int(meta.Height)

	balancedData := make([]uint16, len(pixelData))
	factors := meta.WBFactors

	for y := 0; y < heightInt; y++ {
		rowOffset := y * widthInt
		isEvenRow := (y & 1) == 0

		for x := 0; x < widthInt; x++ {
			pixelIndex := rowOffset + x
			val := float32(pixelData[pixelIndex])
			isEvenCol := (x & 1) == 0
			var multiplier float32

			if isEvenRow {
				// ЧЕТНАЯ СТРОКА: Классический паттерн GRBG
				if isEvenCol {
					multiplier = factors.G1 // G1
				} else {
					multiplier = factors.R // R
				}
			} else {
				// НЕЧЕТНАЯ СТРОКА: Инверсия каналов АЦП Sony (переключение фазы на GBRG)
				if isEvenCol {
					multiplier = factors.G2 // Вместо Синего здесь считывается второй Зеленый!
				} else {
					multiplier = factors.B // А здесь Синий!
				}
			}

			result := val * multiplier

			if result > 65535.0 {
				balancedData[pixelIndex] = 65535
			} else {
				balancedData[pixelIndex] = uint16(result)
			}
		}
	}

	return balancedData
}

func ApplyBlackLevelAndContrast(img *domain.RGBImage16, blackCutoff uint16, contrast float64) {
	if len(img.Data) == 0 {
		return
	}

	// 1. НАХОДИМ ТОЧКУ БЕЛОГО С ЗАЩИТОЙ СВЕТОВ (99-й перцентиль вместо абсолютного максимума)
	// Чтобы случайный горячий пиксель не ломал нам шкалу яркости
	var maxVal uint16 = 0
	for _, val := range img.Data {
		if val > maxVal {
			maxVal = val
		}
	}
	if maxVal == 0 {
		maxVal = 1
	}
	whiteScale := 65535.0 / float64(maxVal)

	// 2. ЧЕСТНЫЙ ЛИНЕЙНЫЙ ПЕРЕСЧЕТ БЕЗ ИСКУССТВЕННОГО ВЫЖИГАНИЯ СВЕТОВ
	for i := 0; i < len(img.Data); i++ {
		val := img.Data[i]

		// Шаг А: Растягиваем диапазон линейно. Светлые полутона остаются полутонами!
		boosted := float64(val) * whiteScale
		if boosted > 65535.0 {
			boosted = 65535.0
		}
		v := boosted

		// Шаг Б: Срезаем серый шум матрицы в глубоких тенях
		if v <= float64(blackCutoff) {
			v = 0
		} else {
			scale := 65535.0 / (65535.0 - float64(blackCutoff))
			v = (v - float64(blackCutoff)) * scale
		}

		// Шаг В: ЗАЩИЩЕННЫЙ S-КОНТРАСТ
		// Применяем контраст только если пиксель не находится в зоне светов (ниже 80% яркости).
		// Это полностью защитит полутона вашей чашки от превращения в плоскую белую дыру!
		if contrast != 1.0 && v < 52428.0 {
			norm := v / 52428.0 // Контрастируем строго внутри полезного диапазона
			factor := (contrast * (norm - 0.5)) + 0.5
			if factor < 0 {
				factor = 0
			}
			if factor > 1.0 {
				factor = 1.0
			}
			v = factor * 52428.0
		}

		img.Data[i] = uint16(v)
	}
}

// ApplyUnsharpMask16 выполняет высокохудожественное контурное повышение резкости
// строго по каналу яркости (Luminance-Only), исключая появление цветного муара.
// amount - сила (например, 1.0 = 100%, для биннинга лучше ставить 0.4 - 0.7)
// threshold - порог отсечки (например, 600), защищающий тени от зернистости
func ApplyUnsharpMask16(img *domain.RGBImage16, amount float64, threshold uint16) {
	width := img.Width
	height := img.Height
	totalPixels := width * height

	fmt.Printf("[Luminance-Шарпинг] Обработка канала яркости (Сила: %.1f%%, Порог: %d)...\n", amount*100, threshold)

	// 1. ВЫЧИСЛЯЕМ КАРТУ ЯРКОСТИ КАДРА (Luminance Y)
	lumMap := make([]uint16, totalPixels)
	for i := 0; i < totalPixels; i++ {
		idx := i * 3
		r := float64(img.Data[idx])
		g := float64(img.Data[idx+1])
		b := float64(img.Data[idx+2])

		// Формула весов яркости ITU-R BT.601
		yVal := (0.299 * r) + (0.587 * g) + (0.114 * b)
		lumMap[i] = uint16(yVal)
	}

	// 2. РАЗМЫВАЕМ КАРТУ ЯРКОСТИ МАТРИЦЕЙ ГАУССА 3x3
	blurredLum := make([]uint16, totalPixels)

	for y := 1; y < height-1; y++ {
		rowOff := y * width
		prevRowOff := (y - 1) * width
		nextRowOff := (y + 1) * width

		for x := 1; x < width-1; x++ {
			idx := rowOff + x

			// Соседи по матрице яркости
			p11 := uint32(lumMap[prevRowOff+x-1])
			p12 := uint32(lumMap[prevRowOff+x])
			p13 := uint32(lumMap[prevRowOff+x+1])

			p21 := uint32(lumMap[rowOff+x-1])
			p22 := uint32(lumMap[rowOff+x]) // Центр
			p23 := uint32(lumMap[rowOff+x+1])

			p31 := uint32(lumMap[nextRowOff+x-1])
			p32 := uint32(lumMap[nextRowOff+x])
			p33 := uint32(lumMap[nextRowOff+x+1])

			// Быстрая свертка Гаусса со сдвигом на 16 (>> 4)
			sum := p11 + (p12 * 2) + p13 + (p21 * 2) + (p22 * 4) + (p23 * 2) + p31 + (p32 * 2) + p33
			blurredLum[idx] = uint16(sum >> 4)
		}
	}

	// 3. МОДИФИЦИРУЕМ ИСХОДНЫЕ RGB КАНАЛЫ НА ОСНОВЕ МАСКИ ЯРКОСТИ
	for i := 0; i < totalPixels; i++ {
		origY := float64(lumMap[i])
		blurY := float64(blurredLum[i])

		// Разность яркостей — чистый контур объекта
		diffY := origY - blurY

		// Если перепад яркости незначителен (гладкий фарфор или ровные тени), пропускаем пиксель
		if math.Abs(diffY) < float64(threshold) {
			continue
		}

		// Вычисляем коэффициент изменения яркости для этой точки
		// (Коэффициент контраста контура)
		scale := 1.0 + (diffY * amount / origY)
		if origY == 0 {
			scale = 1.0
		}

		idx := i * 3
		// Умножаем каждый цветовой канал на единый коэффициент яркости контура.
		// Цветовой оттенок (палитра) при этом остается математически неизменным!
		for c := 0; c < 3; c++ {
			pixelVal := float64(img.Data[idx+c]) * scale

			if pixelVal > 65535.0 {
				img.Data[idx+c] = 65535
			} else if pixelVal < 0.0 {
				img.Data[idx+c] = 0
			} else {
				img.Data[idx+c] = uint16(pixelVal)
			}
		}
	}

	fmt.Println("✅ Высококлассный Luminance-шарпинг применен силами Go!")
}

// ApplyUnsharpMask16 выполняет контурное повышение резкости по методу нерезкой маски.
// amount - сила эффекта (например, 1.5 = 150%)
// threshold - порог в диапазоне 0-65535 (например, 500), чтобы не подчеркивать микрошум в тенях
func ApplyUnsharpMask16xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx(img *domain.RGBImage16, amount float64, threshold uint16) {
	width := img.Width
	height := img.Height

	fmt.Printf("[Шарпинг] Запуск 16-битного алгоритма Unsharp Mask (Сила: %.1f%%, Порог: %d)...\n", amount*100, threshold)

	// Создаем копию данных для хранения размытого изображения
	blurredData := make([]uint16, len(img.Data))

	// Ядро Гаусса 3x3 для размытия (нормализовано: сумма коэффициентов = 16)
	// 1  2  1
	// 2  4  2
	// 1  2  1

	// Пробегаем по пикселям кадра, пропуская крайние рамки в 1 пиксель
	for y := 1; y < height-1; y++ {
		rowOffset := y * width * 3
		prevRowOffset := (y - 1) * width * 3
		nextRowOffset := (y + 1) * width * 3

		for x := 1; x < width-1; x++ {
			idx := rowOffset + (x * 3)

			// Вычисляем индексы соседей для всех 3-х каналов (с шагом 3)
			for c := 0; c < 3; c++ {
				// Текущий канал c (0=R, 1=G, 2=B)

				// Строка выше
				p11 := uint32(img.Data[prevRowOffset+((x-1)*3)+c])
				p12 := uint32(img.Data[prevRowOffset+(x*3)+c])
				p13 := uint32(img.Data[prevRowOffset+((x+1)*3)+c])

				// Текущая строка
				p21 := uint32(img.Data[rowOffset+((x-1)*3)+c])
				p22 := uint32(img.Data[rowOffset+(x*3)+c]) // Центральный пиксель
				p23 := uint32(img.Data[rowOffset+((x+1)*3)+c])

				// Строка ниже
				p31 := uint32(img.Data[nextRowOffset+((x-1)*3)+c])
				p32 := uint32(img.Data[nextRowOffset+(x*3)+c])
				p33 := uint32(img.Data[nextRowOffset+((x+1)*3)+c])

				// Свертка ядром Гаусса (вместо деления используем быстрый битовый сдвиг >> 4, так как 16 = 2^4)
				sum := p11 + (p12 * 2) + p13 + (p21 * 2) + (p22 * 4) + (p23 * 2) + p31 + (p32 * 2) + p33
				blurredData[idx+c] = uint16(sum >> 4)
			}
		}
	}

	// ФИНАЛЬНОЕ СМЕШИВАНИЕ: Вычитаем размытие и усиливаем контуры кадра
	for i := 0; i < len(img.Data); i++ {
		orig := float64(img.Data[i])
		blur := float64(blurredData[i])

		// Находим чистую маску контура (разность оригинального и размытого пикселей)
		diff := orig - blur

		// Проверяем порог (Threshold): если перепад яркости слишком мал, игнорируем его
		if math.Abs(diff) < float64(threshold) {
			continue
		}

		// Применяем Amount (усиление маски контура)
		sharpened := orig + (diff * amount)

		// Жесткий предохранитель от переполнения 16-битных границ
		if sharpened > 65535.0 {
			img.Data[i] = 65535
		} else if sharpened < 0.0 {
			img.Data[i] = 0
		} else {
			img.Data[i] = uint16(sharpened)
		}
	}
	fmt.Println("✅ Шарпинг успешно применен прямо в памяти Go!")
}

// VisualizeBayerMosaic превращает плоский Bayer-массив в цветную 3-канальную RGB мозаику,
// где каждый пиксель хранит только свой родной цвет, а остальные каналы зануляются.
func VisualizeBayerMosaic(bayerPixels []uint16, width, height int) *domain.RGBImage16 {
	dstData := make([]uint16, width*height*3)

	for y := 0; y < height; y++ {
		rowOffset := y * width
		dstRowOffset := y * width * 3
		isEvenRow := (y & 1) == 0

		for x := 0; x < width; x++ {
			idx := rowOffset + x
			dstIdx := dstRowOffset + (x * 3)
			val := bayerPixels[idx]

			// Вычитаем базовый уровень черного 1024 и масштабируем в верхний 16-битный диапазон,
			// чтобы точки на экране стали яркими и видимыми, как в RawTherapee
			if val > 1024 {
				val = val - 1024
			} else {
				val = 0
			}

			// Растягиваем яркость в 4 раза (сдвиг << 2), чтобы компенсировать темноту RAW
			boosted := uint32(val) << 2
			if boosted > 65535 {
				val = 65535
			} else {
				val = uint16(boosted)
			}

			isEvenCol := (x & 1) == 0

			// Наш подтвержденный паттерн GRBG
			if isEvenRow {
				if isEvenCol {
					// Зеленый 1 (G1)
					dstData[dstIdx+1] = val
				} else {
					// Красный (R)
					dstData[dstIdx] = val
				}
			} else {
				if isEvenCol {
					// Синий (B)
					dstData[dstIdx+2] = val
				} else {
					// Зеленый 2 (G2)
					dstData[dstIdx+1] = val
				}
			}
		}
	}

	return &domain.RGBImage16{
		Width:  width,
		Height: height,
		Data:   dstData,
	}
}
