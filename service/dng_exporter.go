package service

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/claygod/rafparser/domain"
)

// compressRGBToBayer берет "разбухший" трехканальный RGB массив биннинга
// и сжимает его в 3 раза, раскладывая цвета обратно в одноканальную мозаику GRBG.
func compressRGBToBayer(rgbData []uint16, width, height int) []uint16 {
	// Итоговый массив будет строго одноканальным (в 3 раза меньше по памяти!)
	bayerData := make([]uint16, width*height)

	for y := 0; y < height; y++ {
		rowOffsetRGB := y * width * 3
		rowOffsetBayer := y * width
		isEvenRow := (y & 1) == 0

		for x := 0; x < width; x++ {
			idxRGB := rowOffsetRGB + (x * 3)
			idxBayer := rowOffsetBayer + x

			rVal := rgbData[idxRGB]
			gVal := rgbData[idxRGB+1]
			bVal := rgbData[idxRGB+2]

			isEvenCol := (x & 1) == 0

			// Укладываем каналы обратно в жесткую сетку паттерна GRBG,
			// но теперь это "честные", очищенные от демозаика цвета!
			if isEvenRow {
				if isEvenCol {
					bayerData[idxBayer] = gVal // Зеленый 1 (G1)
				} else {
					bayerData[idxBayer] = rVal // Красный (R)
				}
			} else {
				if isEvenCol {
					bayerData[idxBayer] = bVal // Синий (B)
				} else {
					bayerData[idxBayer] = gVal // Зеленый 2 (G2)
				}
			}
		}
	}
	return bayerData
}

// ExportToLinearDNG сохраняет RGB-массив биннинга в Linear DNG,
// предварительно сжимая данные в честную одноканальную CFA-мозаику.
func ExportToLinearDNG222(img *domain.RGBImage16, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("не удалось создать DNG файл: %w", err)
	}
	defer file.Close()

	width := uint32(img.Width)
	height := uint32(img.Height)

	fmt.Printf("[Go-DNG] Запись честного Linear DNG: %dx%d...\n", width, height)

	// Заголовок TIFF (Little Endian)
	header := []byte{0x49, 0x49, 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00}
	if _, err := file.Write(header); err != nil {
		return err
	}

	// Смещение длинных данных для 12 тегов
	// 2 + 12 * 12 + 4 = 150 байт. Данные начнутся на смещении 8 + 150 = 158.
	var currentDataOffset uint32 = 158

	colorMatrixDataOffset := currentDataOffset
	currentDataOffset += 72 // 9 Rational * 8 байт

	cfaPatternOffset := currentDataOffset
	currentDataOffset += 4 // 4 байта под паттерн GRBG

	stripOffsetsOffset := currentDataOffset
	currentDataOffset += 4

	stripByteCountsOffset := currentDataOffset
	currentDataOffset += 4

	pixelDataOffset := currentDataOffset

	// ТАБЛИЦА IFD (12 ТЕГОВ, СТРОГО ПО ВОЗРАСТАНИЮ ID)
	tagsCount := uint16(12)
	binary.Write(file, binary.LittleEndian, tagsCount)

	writeTag := func(id uint16, dataType uint16, count uint32, valOffset uint32) {
		binary.Write(file, binary.LittleEndian, id)
		binary.Write(file, binary.LittleEndian, dataType)
		binary.Write(file, binary.LittleEndian, count)
		binary.Write(file, binary.LittleEndian, valOffset)
	}

	writeTag(0x00FE, 4, 1, 1)                     // NewSubFileType (1 = Главный кадр)
	writeTag(0x0100, 4, 1, width)                 // ImageWidth (Истинная половинная ширина)
	writeTag(0x0101, 4, 1, height)                // ImageLength (Истинная половинная высота)
	writeTag(0x0102, 3, 1, 16<<16)                // BitsPerSample (16 бит)
	writeTag(0x0103, 3, 1, 1<<16)                 // Compression (1 = Uncompressed)
	writeTag(0x0106, 3, 1, 34892<<16)             // PhotometricInterpretation (34892 = Linear RAW)
	writeTag(0x0111, 4, 1, stripOffsetsOffset)    // StripOffsets
	writeTag(0x0115, 3, 1, 1<<16)                 // SamplesPerPixel (СТРОГО 1 КАНАЛ ДЛЯ LINEAR RAW!)
	writeTag(0x0117, 4, 1, stripByteCountsOffset) // StripByteCounts
	writeTag(0x011A, 5, 1, 72)                    // XResolution
	writeTag(0x828E, 7, 4, cfaPatternOffset)      // CFARepeatPatternDim (Паттерн сетки 2x2)
	writeTag(0xC621, 5, 9, colorMatrixDataOffset) // ColorMatrix1 (Цветовой паспорт)

	binary.Write(file, binary.LittleEndian, uint32(0))

	// ЗАПИСЬ ВЫНЕСЕННЫХ ДАННЫХ
	matrix := []float64{
		0.6974, -0.1741, -0.0638,
		-0.4912, 1.2384, 0.2858,
		-0.0913, 0.2104, 0.6471,
	}
	for _, val := range matrix {
		binary.Write(file, binary.LittleEndian, int32(val*10000.0))
		binary.Write(file, binary.LittleEndian, int32(10000))
	}

	// Записываем паттерн CFAPattern для геометрии GRBG: 1=Green, 0=Red, 2=Blue, 1=Green
	binary.Write(file, binary.LittleEndian, []byte{1, 0, 2, 1})

	// Указатели смещений пикселей
	binary.Write(file, binary.LittleEndian, pixelDataOffset)

	// Теперь вес файла честный: 1 канал * 2 байта
	totalPixelBytes := width * height * 2
	binary.Write(file, binary.LittleEndian, totalPixelBytes)

	// ОБЕРТКА КОРРЕКЦИИ: Сжимаем "разбухший" RGB-массив обратно в 1-канальный Bayer
	fmt.Println("[Go-DNG] Запуск метода коррекции (сжатие RGB -> одноканальный GRBG)...")
	compressedBayer := compressRGBToBayer(img.Data, int(width), int(height))

	// Записываем сжатый, математически идеальный одноканальный массив на диск
	err = binary.Write(file, binary.LittleEndian, compressedBayer)
	if err != nil {
		return fmt.Errorf("ошибка бинарной записи пикселей в DNG: %w", err)
	}

	fmt.Printf("ПОЛНАЯ ПОБЕДА: Честный половинный Linear DNG '%s' успешно создан!\n", filename)
	return nil
}

// ExportToLinearDNG сохраняет RGB-массив биннинга в Linear DNG.
// Базовая стабильная версия, которая мгновенно открывается в RawTherapee.
func ExportToLinearDNG(img *domain.RGBImage16, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("не удалось создать DNG файл: %w", err)
	}
	defer file.Close()

	width := uint32(img.Width)
	height := uint32(img.Height)

	fmt.Printf("[Go-DNG] Запись базового Linear DNG: %dx%d...\n", width, height)

	// Заголовок TIFF (Little Endian)
	header := []byte{0x49, 0x49, 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00}
	if _, err := file.Write(header); err != nil {
		return err
	}

	// Смещение длинных данных. Ровно 11 тегов.
	// Размер таблицы: 2 + 11 * 12 + 4 = 138 байт. Данные начнутся на смещении 8 + 138 = 146.
	var currentDataOffset uint32 = 146

	bitsPerSampleOffset := currentDataOffset
	currentDataOffset += 6 // 3 SHORT * 2 байта

	stripOffsetsOffset := currentDataOffset
	currentDataOffset += 4

	stripByteCountsOffset := currentDataOffset
	currentDataOffset += 4

	pixelDataOffset := currentDataOffset

	// ЗАПИСЬ ТАБЛИЦЫ IFD (11 ТЕГОВ, СТРОГО ПО ВОЗРАСТАНИЮ ID)
	tagsCount := uint16(11)
	binary.Write(file, binary.LittleEndian, tagsCount)

	writeTag := func(id uint16, dataType uint16, count uint32, valOffset uint32) {
		binary.Write(file, binary.LittleEndian, id)
		binary.Write(file, binary.LittleEndian, dataType)
		binary.Write(file, binary.LittleEndian, count)
		binary.Write(file, binary.LittleEndian, valOffset)
	}

	writeTag(0x00FE, 4, 1, 1)                     // NewSubFileType
	writeTag(0x0100, 4, 1, width)                 // ImageWidth
	writeTag(0x0101, 4, 1, height)                // ImageLength
	writeTag(0x0102, 3, 3, bitsPerSampleOffset)   // BitsPerSample
	writeTag(0x0103, 3, 1, 1<<16)                 // Compression (1 = Uncompressed)
	writeTag(0x0106, 3, 1, 34892<<16)             // PhotometricInterpretation (34892 = ВЕРНУЛИ LINEAR RAW!)
	writeTag(0x0111, 4, 1, stripOffsetsOffset)    // StripOffsets
	writeTag(0x0115, 3, 1, 3<<16)                 // SamplesPerPixel (3 канала)
	writeTag(0x0117, 4, 1, stripByteCountsOffset) // StripByteCounts
	writeTag(0x011A, 5, 1, 72)                    // XResolution (Заглушка)
	writeTag(0xC61D, 4, 1, 65535)                 // WhiteLevel (65535)

	// Конец IFD таблицы
	binary.Write(file, binary.LittleEndian, uint32(0))

	// Запись BitsPerSample (16, 16, 16)
	binary.Write(file, binary.LittleEndian, uint16(16))
	binary.Write(file, binary.LittleEndian, uint16(16))
	binary.Write(file, binary.LittleEndian, uint16(16))

	// Указатели смещений и веса пикселей
	binary.Write(file, binary.LittleEndian, pixelDataOffset)

	totalPixelBytes := width * height * 3 * 2
	binary.Write(file, binary.LittleEndian, totalPixelBytes)

	// Запись тяжелого массива пикселей биннинга
	err = binary.Write(file, binary.LittleEndian, compressRGBToBayer(img.Data, int(width), int(height)))
	if err != nil {
		return fmt.Errorf("ошибка записи пикселей: %w", err)
	}

	return nil
}
