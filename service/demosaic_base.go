package service

import (
	"fmt"

	"github.com/claygod/rafparser/domain"
)

// BilinearGRBG выполняет дебайеризацию (демозаику) плоского массива пикселей
// методом билинейной интерполяции для паттерна GRBG.
func BilinearGRBG(pixelData []uint16, meta *domain.GFXMetadata) *domain.RGBImage16 {
	width := int(meta.Width)
	height := int(meta.Height)
	totalPixels := width * height

	// Выделяем один большой плоский RGB-массив (утраиваем размер)
	rgbData := make([]uint16, totalPixels*3)

	// Цикл прохода по всей матрице
	for y := 0; y < height; y++ {
		rowOffset := y * width
		isEvenRow := (y & 1) == 0

		for x := 0; x < width; x++ {
			// Индекс текущего пикселя в исходном ч/б массиве
			idx := rowOffset + x
			// Стартовый индекс этого же пикселя в новом RGB массиве [R=0, G=1, B=2]
			rgbIdx := idx * 3

			// Предохранитель для краев кадра (граница в 1 пиксель)
			// На краях интерполяция сложная, поэтому просто копируем исходный цвет во все каналы (серая точка)
			if x == 0 || y == 0 || x == width-1 || y == height-1 {
				val := pixelData[idx]
				rgbData[rgbIdx] = val   // R
				rgbData[rgbIdx+1] = val // G
				rgbData[rgbIdx+2] = val // B
				continue
			}

			isEvenCol := (x & 1) == 0

			//--

			// // Логика интерполяции для паттерна BGGR
			// if isEvenRow {
			// 	if isEvenCol {
			// 		// 1. ПИКСЕЛЬ СИНИЙ (B)
			// 		rgbData[rgbIdx+2] = pixelData[idx]
			// 		rgbData[rgbIdx+1] = (pixelData[idx-1] + pixelData[idx+1] + pixelData[idx-width] + pixelData[idx+width]) >> 2
			// 		rgbData[rgbIdx] = (pixelData[idx-width-1] + pixelData[idx-width+1] + pixelData[idx+width-1] + pixelData[idx+width+1]) >> 2
			// 	} else {
			// 		// 2. ПИКСЕЛЬ ЗЕЛЕНЫЙ (G2)
			// 		rgbData[rgbIdx+1] = pixelData[idx]
			// 		rgbData[rgbIdx] = (pixelData[idx-width] + pixelData[idx+width]) >> 1
			// 		rgbData[rgbIdx+2] = (pixelData[idx-1] + pixelData[idx+1]) >> 1
			// 	}
			// } else {
			// 	if isEvenCol {
			// 		// 3. ПИКСЕЛЬ ЗЕЛЕНЫЙ (G1)
			// 		rgbData[rgbIdx+1] = pixelData[idx]
			// 		rgbData[rgbIdx] = (pixelData[idx-1] + pixelData[idx+1]) >> 1
			// 		rgbData[rgbIdx+2] = (pixelData[idx-width] + pixelData[idx+width]) >> 1
			// 	} else {
			// 		// 4. ПИКСЕЛЬ КРАСНЫЙ (R)
			// 		rgbData[rgbIdx] = pixelData[idx]
			// 		rgbData[rgbIdx+1] = (pixelData[idx-1] + pixelData[idx+1] + pixelData[idx-width] + pixelData[idx+width]) >> 2
			// 		rgbData[rgbIdx+2] = (pixelData[idx-width-1] + pixelData[idx-width+1] + pixelData[idx+width-1] + pixelData[idx+width+1]) >> 2
			// 	}
			// }

			// Измените внутренний блок условий в BilinearGRBG под чередование каналов GRBG / GBRG
			if isEvenRow {
				if isEvenCol {
					// 1. Четная строка / Четный кол -> G1
					rgbData[rgbIdx+1] = pixelData[idx]
					rgbData[rgbIdx] = (pixelData[idx-1] + pixelData[idx+1]) >> 1
					rgbData[rgbIdx+2] = (pixelData[idx-width] + pixelData[idx+width]) >> 1
				} else {
					// 2. Четная строка / Нечетный кол -> R
					rgbData[rgbIdx] = pixelData[idx]
					rgbData[rgbIdx+1] = (pixelData[idx-1] + pixelData[idx+1] + pixelData[idx-width] + pixelData[idx+width]) >> 2
					rgbData[rgbIdx+2] = (pixelData[idx-width-1] + pixelData[idx-width+1] + pixelData[idx+width-1] + pixelData[idx+width+1]) >> 2
				}
			} else {
				if isEvenCol {
					// 3. Нечетная строка / Четный кол -> G2 (Вместо B)
					rgbData[rgbIdx+1] = pixelData[idx]
					rgbData[rgbIdx] = (pixelData[idx-width] + pixelData[idx+width]) >> 1
					rgbData[rgbIdx+2] = (pixelData[idx-1] + pixelData[idx+1]) >> 1
				} else {
					// 4. Нечетная строка / Нечетный кол -> B (Вместо G2)
					rgbData[rgbIdx+2] = pixelData[idx]
					rgbData[rgbIdx+1] = (pixelData[idx-1] + pixelData[idx+1] + pixelData[idx-width] + pixelData[idx+width]) >> 2
					rgbData[rgbIdx] = (pixelData[idx-width-1] + pixelData[idx-width+1] + pixelData[idx+width-1] + pixelData[idx+width+1]) >> 2
				}
			}

			// Логика интерполяции для паттерна GRBG
			// if isEvenRow {
			// 	if isEvenCol {
			// 		// 1. ПИКСЕЛЬ ЗЕЛЕНЫЙ (G1)
			// 		// Строка четная, столбец четный. Окружение: слева/справа - R, сверху/снизу - B
			// 		rgbData[rgbIdx+1] = pixelData[idx] // Green берем как есть

			// 		// Red — среднее между левым и правым соседом
			// 		rgbData[rgbIdx] = (pixelData[idx-1] + pixelData[idx+1]) >> 1

			// 		// Blue — среднее между верхним и нижним соседом
			// 		rgbData[rgbIdx+2] = (pixelData[idx-width] + pixelData[idx+width]) >> 1

			// 	} else {
			// 		// 2. ПИКСЕЛЬ КРАСНЫЙ (R)
			// 		// Строка четная, столбец нечетный. Окружение: крест из G, по диагоналям - B
			// 		rgbData[rgbIdx] = pixelData[idx] // Red берем как есть

			// 		// Green — среднее арифметическое 4-х соседей крестом (сдвиг >> 2 равен делению на 4)
			// 		rgbData[rgbIdx+1] = (pixelData[idx-1] + pixelData[idx+1] + pixelData[idx-width] + pixelData[idx+width]) >> 2

			// 		// Blue — среднее 4-х соседей по диагоналям
			// 		rgbData[rgbIdx+2] = (pixelData[idx-width-1] + pixelData[idx-width+1] + pixelData[idx+width-1] + pixelData[idx+width+1]) >> 2
			// 	}
			// } else {
			// 	if isEvenCol {
			// 		// 3. ПИКСЕЛЬ СИНИЙ (B)
			// 		// Строка нечетная, столбец четный. Окружение: крест из G, по диагоналям - R
			// 		rgbData[rgbIdx+2] = pixelData[idx] // Blue берем как есть

			// 		// Green — среднее арифметическое 4-х соседей крестом
			// 		rgbData[rgbIdx+1] = (pixelData[idx-1] + pixelData[idx+1] + pixelData[idx-width] + pixelData[idx+width]) >> 2

			// 		// Red — среднее 4-х соседей по диагоналям
			// 		rgbData[rgbIdx] = (pixelData[idx-width-1] + pixelData[idx-width+1] + pixelData[idx+width-1] + pixelData[idx+width+1]) >> 2

			// 	} else {
			// 		// 4. ПИКСЕЛЬ ЗЕЛЕНЫЙ (G2)
			// 		// Строка нечетная, столбец нечетный. Окружение: слева/справа - B, сверху/снизу - R
			// 		rgbData[rgbIdx+1] = pixelData[idx] // Green берем как есть

			// 		// Red — среднее между верхним и нижним соседом
			// 		rgbData[rgbIdx] = (pixelData[idx-width] + pixelData[idx+width]) >> 1

			// 		// Blue — среднее между левым и правым соседом
			// 		rgbData[rgbIdx+2] = (pixelData[idx-1] + pixelData[idx+1]) >> 1
			// 	}
			// }
		}
	}

	return &domain.RGBImage16{
		Width:  width,
		Height: height,
		Data:   rgbData,
	}
}

// BayerBinning2x2 берет сырой одноканальный Bayer-массив GRBG,
// схлопывает блоки 2x2 пикселя в один честный RGB-пиксель половинного разрешения,
// полностью исключая необходимость в интерполяции (демозаике).
func BayerBinning2x2(bayerPixels []uint16, meta *domain.GFXMetadata) *domain.RGBImage16 {
	srcWidth := int(meta.Width)
	srcHeight := int(meta.Height)

	// Новая геометрия кадра (ровно в 2 раза меньше)
	// Для GFX100 это будет 11648/2 = 5824 и 8736/2 = 4368
	dstWidth := srcWidth / 2
	dstHeight := srcHeight / 2

	fmt.Printf("[Биннинг] Схлопывание матрицы %dx%d -> %dx%d (Честный RGB без демозаика)\n",
		srcWidth, srcHeight, dstWidth, dstHeight)

	// Выделяем память под итоговое 3-канальное изображение половинного разрешения
	dstData := make([]uint16, dstWidth*dstHeight*3)

	// Обходим матрицу блоками 2x2 пикселя
	for y := 0; y < dstHeight; y++ {
		srcY0 := y * 2       // Индекс первой строки блока 2x2 в исходной матрице
		srcY1 := (y * 2) + 1 // Индекс второй строки блока 2x2

		dstRowOffset := y * dstWidth * 3

		for x := 0; x < dstWidth; x++ {
			srcX0 := x * 2       // Индекс первого столбца блока
			srcX1 := (x * 2) + 1 // Индекс второго столбца блока

			// // Вычисляем физические одномерные индексы 4-х бáйер-пикселей сенсора Sony
			// // Паттерн GRBG:
			// // Строка 0: G1, R
			// // Строка 1: B,  G2
			// idxG1 := (srcY0 * srcWidth) + srcX0
			// idxR := (srcY0 * srcWidth) + srcX1
			// idxB := (srcY1 * srcWidth) + srcX0
			// idxG2 := (srcY1 * srcWidth) + srcX1

			// Измените расчет индексов внутри циклов функции BayerBinning2x2
			stride := 11648 // Полная физическая ширина строки сенсора

			// СДВИГАЕМ ФАЗУ: Инвертируем чтение строк и столбцов,
			// чтобы точно попасть в физическую раскладку датчиков вашей чашки!
			// Проверим фазу BGGR (Строка 0: B, G2 | Строка 1: G1, R)
			idxB := (srcY0 * stride) + srcX0
			idxG2 := (srcY0 * stride) + srcX1
			idxG1 := (srcY1 * stride) + srcX0
			idxR := (srcY1 * stride) + srcX1

			// 1. Извлекаем чистые значения яркости
			rVal := bayerPixels[idxR]
			bVal := bayerPixels[idxB]
			gVal := (uint32(bayerPixels[idxG1]) + uint32(bayerPixels[idxG2])) / 2

			// 2. Убираем фиксированный уровень черного 1024, который зашит в АЦП GFX100
			// (В сыром массиве LibRaw он равен 1024. Если его не вычесть, картинка будет блеклой!)
			black := uint32(1024)

			var rClean, gClean, bClean uint16
			if uint32(rVal) > black {
				rClean = uint16(uint32(rVal) - black)
			}
			if uint32(gVal) > black {
				gClean = uint16(gVal - black)
			}
			if uint32(bVal) > black {
				bClean = uint16(uint32(bVal) - black)
			}

			// 3. Применяем родной баланс белого камеры кадра
			rBalanced := float32(rClean) * meta.WBFactors.R
			gBalanced := float32(gClean) * ((meta.WBFactors.G1 + meta.WBFactors.G2) / 2.0)
			bBalanced := float32(bClean) * meta.WBFactors.B

			// Записываем «честную» RGB-тройку в итоговый массив
			dstIdx := dstRowOffset + (x * 3)

			// Защита от переполнения (ограничиваем потолком 16 бит — 65535)
			if rBalanced > 65535 {
				dstData[dstIdx] = 65535
			} else {
				dstData[dstIdx] = uint16(rBalanced)
			}
			if gBalanced > 65535 {
				dstData[dstIdx+1] = 65535
			} else {
				dstData[dstIdx+1] = uint16(gBalanced)
			}
			if bBalanced > 65535 {
				dstData[dstIdx+2] = 65535
			} else {
				dstData[dstIdx+2] = uint16(bBalanced)
			}
		}
	}

	return &domain.RGBImage16{
		Width:  dstWidth,
		Height: dstHeight,
		Data:   dstData,
	}
}
