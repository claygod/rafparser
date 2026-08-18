package service

import (
	"fmt"
	"os"

	"github.com/claygod/rafparser/domain"
	"github.com/mrjoshuak/go-openexr/exr"
	"github.com/mrjoshuak/go-openexr/half"
)

// ExportToLinearEXR сохраняет RGB-массив биннинга в оригинальный HDR-формат OpenEXR (.exr).
// Данные нормализуются на основе fujiWhiteLevel во float16 (half) без искусственного клиппинга.
func ExportToLinearEXR(img *domain.RGBImage16, fujiWhiteLevel float32, exposureCompensation float32, filename string) error {
	width := int(img.Width)
	height := int(img.Height)

	fmt.Printf("[Go-EXR] Запись эластичного Linear EXR (PIZ-сжатие): %dx%d...\n", width, height)

	// 1. Инициализируем стандартный сканлайн-заголовок OpenEXR
	header := exr.NewScanlineHeader(width, height)

	// Выбираем волновое сжатие PIZ — оно идеально и без потерь жмет RAW-матрицы
	header.SetCompression(exr.CompressionPIZ)

	// 2. Создаем три раздельных цветовых канала с типом PixelTypeHalf (16-битный float)
	header.Channels().Add(exr.Channel{Name: "R", Type: exr.PixelTypeHalf})
	header.Channels().Add(exr.Channel{Name: "G", Type: exr.PixelTypeHalf})
	header.Channels().Add(exr.Channel{Name: "B", Type: exr.PixelTypeHalf})

	// 3. Выделяем плоские буферы памяти под каждый канал в формате half
	rPixels := make([]half.Half, width*height)
	gPixels := make([]half.Half, width*height)
	bPixels := make([]half.Half, width*height)

	// На случай, если передан нулевой или некорректный white level, страхуем код
	if fujiWhiteLevel <= 0 {
		fujiWhiteLevel = 16000.0
	}

	// // 4. Попиксельно переносим данные из плоского uint16 массива в каналы float16
	// idx := 0
	// for i := 0; i < width*height; i++ {
	// 	// Деление на уровень белого переводит пиксель в диапазон от 0.0 до 1.0.
	// 	// Если пиксель ярче уровня белого камеры, он станет > 1.0 и сохранится без обрезки!
	// 	rFloat := float32(img.Data[idx]) / fujiWhiteLevel
	// 	gFloat := float32(img.Data[idx+1]) / fujiWhiteLevel
	// 	bFloat := float32(img.Data[idx+2]) / fujiWhiteLevel

	// 	rPixels[i] = half.FromFloat32(rFloat)
	// 	gPixels[i] = half.FromFloat32(gFloat)
	// 	bPixels[i] = half.FromFloat32(bFloat)

	// 	idx += 3
	// }

	// Вводим запас для светов (например, 3.5 ступени).
	// Это сдвинет средние тона вниз, сделав базовую экспозицию нормальной,
	// а пиксели, которые были близки к клиппингу камеры, улетят в диапазон > 1.0!

	targetWhitePoint := fujiWhiteLevel * exposureCompensation

	idx := 0
	for i := 0; i < width*height; i++ {
		// Делим на увеличенный порог. Теперь обычные пиксели станут около 0.18 - 0.5,
		// а реальный пересвет сенсора запишется в EXR как значение ~3.5
		rFloat := float32(img.Data[idx]) / targetWhitePoint
		gFloat := float32(img.Data[idx+1]) / targetWhitePoint
		bFloat := float32(img.Data[idx+2]) / targetWhitePoint

		rPixels[i] = half.FromFloat32(rFloat)
		gPixels[i] = half.FromFloat32(gFloat)
		bPixels[i] = half.FromFloat32(bFloat)

		idx += 3
	}

	// 5. Регистрируем слайсы в низкоуровневом фреймбуфере упаковщика
	fb := exr.NewFrameBuffer()
	fb.Insert("R", exr.NewSliceFromHalf(rPixels, width, height))
	fb.Insert("G", exr.NewSliceFromHalf(gPixels, width, height))
	fb.Insert("B", exr.NewSliceFromHalf(bPixels, width, height))

	// 6. Физически пишем файл на диск последовательными чанками
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("не удалось создать EXR файл: %w", err)
	}
	defer file.Close()

	writer, err := exr.NewScanlineWriter(file, header)
	if err != nil {
		return fmt.Errorf("ошибка инициализации EXR-писателя: %w", err)
	}
	writer.SetFrameBuffer(fb)

	// Передаем инклюзивный диапазон строк для записи
	if err := writer.WritePixels(0, height-1); err != nil {
		return fmt.Errorf("ошибка записи пикселей в EXR: %w", err)
	}

	// Фиксируем итоговую таблицу смещений блоков (обязательный вызов!)
	if err := writer.Close(); err != nil {
		return fmt.Errorf("ошибка финального закрытия EXR-писателя: %w", err)
	}

	fmt.Printf("ПОЛНАЯ ПОБЕДА: HDR Linear EXR '%s' успешно сохранен!\n", filename)
	return nil
}
