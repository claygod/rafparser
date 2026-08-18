package service

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/claygod/rafparser/domain"
)

// ExportToLinearDNGTiles упаковывает 3-канальный RGB-массив биннинга в валидный
// плиточный Linear DNG контейнер стандарта Adobe (аналог файлов Foveon X3) без полос и сдвигов.
func ExportToLinearDNGTiles(img *domain.RGBImage16, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("не удалось создать DNG файл: %w", err)
	}
	defer file.Close()

	// Использовать ИСТИННЫЕ геометрические размеры переданного RGB-объекта
	width := uint32(img.Width)
	height := uint32(img.Height)

	const tileSize uint32 = 256 // Размер плитки по спецификации Adobe

	// Расчитываем количество плиток по горизонтали и вертикали с округлением вверх
	tilesX := (width + tileSize - 1) / tileSize
	tilesY := (height + tileSize - 1) / tileSize
	totalTiles := tilesX * tilesY

	fmt.Printf("[Go-DNG-Tiles] Геометрия: %dx%d (Сетка плиток: %dx%d, Всего: %d)\n",
		width, height, tilesX, tilesY, totalTiles)

	// 1. ЗАГОЛОВОК TIFF (Little Endian, 8 байт)
	header := []byte{0x49, 0x49, 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00}
	if _, err := file.Write(header); err != nil {
		return err
	}

	// СТРОГИЙ РАСЧЕТ СМЕЩЕНИЙ ДЛЯ 14 ТЕГОВ
	// Таблица IFD занимает: 2 байта (кол-во) + 14 тегов * 12 байт + 4 байта (next_ifd) = 174 байта.
	// Вынесенные массивы данных начнутся строго на смещении 8 + 174 = 182.
	var currentDataOffset uint32 = 182

	colorMatrixDataOffset := currentDataOffset
	currentDataOffset += 72 // 9 Rational * 8 байт

	bitsPerSampleOffset := currentDataOffset
	currentDataOffset += 8 // 3 SHORT * 2 байта = 6 байт + 2 байта паддинг для выравнивания

	// Массив TileOffsets: по одному uint32 (4 байта) на каждую плитку
	tileOffsetsOffset := currentDataOffset
	currentDataOffset += totalTiles * 4

	// ФИЗИЧЕСКИЙ СТАРТ ТЯЖЕЛЫХ ПИКСЕЛЕЙ В ФАЙЛЕ (ИСПРАВЛЕНО: убрана лишняя переменная!)
	pixelDataOffset := currentDataOffset

	// 2. ЗАПИСЬ ТАБЛИЦЫ IFD (14 ТЕГОВ, СТРОГО ПО ВОЗРАСТАНИЮ ID ТЕГА)
	tagsCount := uint16(14)
	binary.Write(file, binary.LittleEndian, tagsCount)

	writeTag := func(id uint16, dataType uint16, count uint32, valOffset uint32) {
		binary.Write(file, binary.LittleEndian, id)
		binary.Write(file, binary.LittleEndian, dataType)
		binary.Write(file, binary.LittleEndian, count)
		binary.Write(file, binary.LittleEndian, valOffset)
	}

	const tileBytes uint32 = tileSize * tileSize * 3 * 2 // Вес одной плитки 256x256x3x2 = 393216 байт

	writeTag(0x00FE, 4, 1, 1)                          // NewSubFileType
	writeTag(0x0100, 4, 1, width)                      // ImageWidth
	writeTag(0x0101, 4, 1, height)                     // ImageLength
	writeTag(0x0102, 3, 3, bitsPerSampleOffset)        // BitsPerSample
	writeTag(0x0103, 3, 1, 1<<16)                      // Compression (1 = Uncompressed)
	writeTag(0x0106, 3, 1, 34892<<16)                  // PhotometricInterpretation (Linear RAW)
	writeTag(0x0115, 3, 1, 3<<16)                      // SamplesPerPixel (3 канала RGB)
	writeTag(0x011A, 5, 1, 72)                         // XResolution
	writeTag(0x0142, 4, 1, tileSize)                   // TileWidth
	writeTag(0x0143, 4, 1, tileSize)                   // TileLength
	writeTag(0x0144, 4, totalTiles, tileOffsetsOffset) // TileOffsets (Массив смещений плиток)
	writeTag(0x0145, 4, 1, tileBytes)                  // TileByteCounts (Фиксированное значение для всех плит)
	writeTag(0xC61D, 4, 1, 65535)                      // WhiteLevel (65535)
	writeTag(0xC621, 5, 9, colorMatrixDataOffset)      // ColorMatrix1

	binary.Write(file, binary.LittleEndian, uint32(0))

	// 3. ЗАПИСЬ ВЫНЕСЕННЫХ ДАННЫХ МЕТА-ТЕГОВ (На смещении 182)
	matrix := []float64{
		0.6974, -0.1741, -0.0638,
		-0.4912, 1.2384, 0.2858,
		-0.0913, 0.2104, 0.6471,
	}
	for _, val := range matrix {
		binary.Write(file, binary.LittleEndian, int32(val*10000.0))
		binary.Write(file, binary.LittleEndian, int32(10000))
	}

	binary.Write(file, binary.LittleEndian, uint16(16))
	binary.Write(file, binary.LittleEndian, uint16(16))
	binary.Write(file, binary.LittleEndian, uint16(16))
	binary.Write(file, binary.LittleEndian, uint16(0)) // Паддинг

	// Записываем TileOffsets
	for i := uint32(0); i < totalTiles; i++ {
		offset := pixelDataOffset + (i * tileBytes)
		binary.Write(file, binary.LittleEndian, offset)
	}

	// 4. БЕЗОПАСНАЯ ДВУМЕРНАЯ УПАКОВКА ПИКСЕЛЕЙ ПО КОРОБКАМ
	fmt.Println("[Go-DNG-Tiles] Нарезка плиток по каноническому шагу строк...")

	tileBuffer := make([]uint16, tileSize*tileSize*3)

	for ty := uint32(0); ty < tilesY; ty++ {
		for tx := uint32(0); tx < tilesX; tx++ {

			// Обнуляем буфер плитки
			for i := range tileBuffer {
				tileBuffer[i] = 0
			}

			startX := tx * tileSize
			startY := ty * tileSize

			for subY := uint32(0); subY < tileSize; subY++ {
				globalY := startY + subY
				if globalY >= height {
					break // Нижний край
				}

				// ТОЧНЫЙ РАСЧЕТ ШАГА СТРОКИ: берем исходную ширину img.Width
				globalRowOffset := int(globalY) * img.Width * 3
				tileRowOffset := subY * tileSize * 3

				for subX := uint32(0); subX < tileSize; subX++ {
					globalX := startX + subX
					if globalX >= width {
						break // Правый край
					}

					// ЮВЕЛИРНЫЙ ПЕРЕНОС: Строгий контроль физического адреса пикселя в Go
					gIdx := globalRowOffset + (int(globalX) * 3)
					tIdx := tileRowOffset + (subX * 3)

					tileBuffer[tIdx] = img.Data[gIdx]     // R
					tileBuffer[tIdx+1] = img.Data[gIdx+1] // G
					tileBuffer[tIdx+2] = img.Data[gIdx+2] // B
				}
			}

			// Выгружаем готовую плитку на диск
			if err := binary.Write(file, binary.LittleEndian, tileBuffer); err != nil {
				return fmt.Errorf("ошибка записи плитки: %w", err)
			}
		}
	}

	fmt.Printf("🏆 ГЕОМЕТРИЯ СОСТЫКОВАНА: Плиточный Linear DNG '%s' готов!\n", filename)
	return nil
}

// ExportToLinearDNGTilesDiagnostic генерирует синтетический тестовый DNG файл,
// где каждая плитка 256x256 залита своим уникальным контрастным цветом.
// Помогает визуально определить геометрические и фазовые искажения парсера RT.
func ExportToLinearDNGTilesDiagnostic(filename string) error {
	// Жестко задаем размеры кадра для теста (половинное разрешение нашей GFX)
	const width uint32 = 5832
	const height uint32 = 4375
	const tileSize uint32 = 256

	// Расчитываем сетку плиток
	tilesX := (width + tileSize - 1) / tileSize
	tilesY := (height + tileSize - 1) / tileSize
	totalTiles := tilesX * tilesY

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("не удалось создать диагностический DNG: %w", err)
	}
	defer file.Close()

	fmt.Printf("[💥 Диагностика] Генерация тестового DNG: %dx%d (Сетка плиток: %dx%d, Всего: %d)\n",
		width, height, tilesX, tilesY, totalTiles)

	// 1. ЗАГОЛОВОК TIFF (Little Endian, 8 байт)
	header := []byte{0x49, 0x49, 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00}
	if _, err := file.Write(header); err != nil {
		return err
	}

	// СТРОГИЙ РАСЧЕТ СМЕЩЕНИЙ
	var currentDataOffset uint32 = 182

	colorMatrixDataOffset := currentDataOffset
	currentDataOffset += 72 // 9 Rational * 8 байт

	bitsPerSampleOffset := currentDataOffset
	currentDataOffset += 8 // 3 SHORT * 2 байта + 2 паддинг

	tileOffsetsOffset := currentDataOffset
	currentDataOffset += totalTiles * 4

	pixelDataOffset := currentDataOffset

	// 2. ЗАПИСЬ ТАБЛИЦЫ IFD (14 ТЕГОВ, СТРОГО ПО ВОЗРАСТАНИЮ ID)
	tagsCount := uint16(14)
	binary.Write(file, binary.LittleEndian, tagsCount)

	writeTag := func(id uint16, dataType uint16, count uint32, valOffset uint32) {
		binary.Write(file, binary.LittleEndian, id)
		binary.Write(file, binary.LittleEndian, dataType)
		binary.Write(file, binary.LittleEndian, count)
		binary.Write(file, binary.LittleEndian, valOffset)
	}

	const tileBytes uint32 = tileSize * tileSize * 3 * 2 // Вес одной плитки 256x256x3x2 = 393216 байт

	writeTag(0x00FE, 4, 1, 1)                          // NewSubFileType
	writeTag(0x0100, 4, 1, width)                      // ImageWidth
	writeTag(0x0101, 4, 1, height)                     // ImageLength
	writeTag(0x0102, 3, 3, bitsPerSampleOffset)        // BitsPerSample
	writeTag(0x0103, 3, 1, 1<<16)                      // Compression (1 = Uncompressed)
	writeTag(0x0106, 3, 1, 34892<<16)                  // PhotometricInterpretation (Linear RAW)
	writeTag(0x0115, 3, 1, 3<<16)                      // SamplesPerPixel (3 канала RGB)
	writeTag(0x011A, 5, 1, 72)                         // XResolution
	writeTag(0x0142, 4, 1, tileSize)                   // TileWidth (256)
	writeTag(0x0143, 4, 1, tileSize)                   // TileLength (256)
	writeTag(0x0144, 4, totalTiles, tileOffsetsOffset) // TileOffsets
	writeTag(0x0145, 4, 1, tileBytes)                  // TileByteCounts
	writeTag(0xC61D, 4, 1, 65535)                      // WhiteLevel
	writeTag(0xC621, 5, 9, colorMatrixDataOffset)      // ColorMatrix1

	binary.Write(file, binary.LittleEndian, uint32(0))

	// 3. ЗАПИСЬ ВЫНЕСЕННЫХ ДАННЫХ
	// Паспортная матрица
	matrix := []float64{
		1.0, 0.0, 0.0,
		0.0, 1.0, 0.0,
		0.0, 0.0, 1.0,
	}
	for _, val := range matrix {
		binary.Write(file, binary.LittleEndian, int32(val*10000.0))
		binary.Write(file, binary.LittleEndian, int32(10000))
	}

	binary.Write(file, binary.LittleEndian, uint16(16))
	binary.Write(file, binary.LittleEndian, uint16(16))
	binary.Write(file, binary.LittleEndian, uint16(16))
	binary.Write(file, binary.LittleEndian, uint16(0)) // Паддинг

	// Записываем TileOffsets
	for i := uint32(0); i < totalTiles; i++ {
		offset := pixelDataOffset + (i * tileBytes)
		binary.Write(file, binary.LittleEndian, offset)
	}

	// 4. ГЕНЕРАЦИЯ ЦВЕТНЫХ КОРОБОК
	tileBuffer := make([]uint16, tileSize*tileSize*3)

	var tileID uint32 = 0
	for ty := uint32(0); ty < tilesY; ty++ {
		for tx := uint32(0); tx < tilesX; tx++ {

			// МАТЕМАТИКА ЦВЕТОВОГО ЗОНДИРОВАНИЯ:
			// Генерируем уникальный, сильно отличающийся оттенок для каждой коробки.
			// Используем остаток от деления и битовые сдвиги, чтобы соседние плитки кардинально отличались.
			rColor := uint16(((tileID * 17) % 255) * 250)
			gColor := uint16(((tileID * 31) % 255) * 250)
			bColor := uint16(((tileID * 53) % 255) * 250)

			// Если это четный ряд и четная строка, принудительно делаем упор на один цвет,
			// чтобы легко было считать строки на экране визуально
			if (ty&1) == 0 && (tx&1) == 0 {
				rColor = 60000
				gColor = 1000
				bColor = 1000
			}

			// Полностью заливаем текущую бинарную коробку этим цветом
			for i := 0; i < len(tileBuffer); i += 3 {
				tileBuffer[i] = rColor   // R
				tileBuffer[i+1] = gColor // G
				tileBuffer[i+2] = bColor // B
			}

			// Пишем коробку целиком на диск
			if err := binary.Write(file, binary.LittleEndian, tileBuffer); err != nil {
				return fmt.Errorf("ошибка записи диагностической плитки: %w", err)
			}

			tileID++
		}
	}

	fmt.Printf("🏆 ТЕСТОВЫЙ НАБОР ГОТОВ: Зонд '%s' успешно сгенерирован!\n", filename)
	return nil
}
