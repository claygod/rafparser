package service

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/claygod/rafparser/domain"
)

// ExportToTIFF16 сохраняет плоский RGB-массив в несжатый 16-битный файл TIFF.
// Использует Little Endian для максимальной скорости прямой записи из памяти.
func ExportToTIFF16(img *domain.RGBImage16, outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("не удалось создать файл TIFF: %w", err)
	}
	defer file.Close()

	// Используем большой буфер для минимизации обращений к диску
	writer := bufio.NewWriterSize(file, 4*1024*1024) // 4 МБ буфер

	// 1. ЗАПИСЬ ЗАГОЛОВКА TIFF (8 байт)
	// "II" — маркер Little Endian (0x49 0x49)
	// 0x002A — магическое число TIFF (42)
	// 0x00000008 — смещение до данных (начинаются сразу за заголовком)
	_, _ = writer.Write([]byte{0x49, 0x49, 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00})

	// 2. ПРЯМАЯ ПОТОКОВАЯ ЗАПИСЬ МАТРИЦЫ ПИКСЕЛЕЙ (Strip Data)
	// Внимание: Переводим uint16 в байты Little Endian
	pixelBytes := make([]byte, 2)
	for _, val := range img.Data {
		pixelBytes[0] = byte(val)
		pixelBytes[1] = byte(val >> 8)
		_, err = writer.Write(pixelBytes)
		if err != nil {
			return fmt.Errorf("ошибка при записи пикселей: %w", err)
		}
	}

	// Вычисляем смещение для таблицы тегов (IFD). Она пойдет сразу за пикселями.
	ifdOffset := uint32(8 + len(img.Data)*2)

	// Вручную дописываем указатель на IFD в заголовок (байты 4-7)
	// Для этого сбросим буфер на диск, временно вернемся в начало и перезапишем адрес
	_ = writer.Flush()
	_, _ = file.Seek(4, io.SeekStart)
	_ = binary.Write(file, binary.LittleEndian, ifdOffset)
	_, _ = file.Seek(0, io.SeekEnd) // Возвращаемся в конец файла
	writer.Reset(file)

	// 3. ЗАПИСЬ ТАБЛИЦЫ ТЕГОВ (IFD - Image File Directory)
	// Количество тегов в нашей структуре: 10
	var numTags uint16 = 10
	_ = binary.Write(writer, binary.LittleEndian, numTags)

	// Вспомогательная функция для записи одного тега (12 байт на тег)
	writeTag := func(tag uint16, dataType uint16, count uint32, value uint32) {
		_ = binary.Write(writer, binary.LittleEndian, tag)
		_ = binary.Write(writer, binary.LittleEndian, dataType)
		_ = binary.Write(writer, binary.LittleEndian, count)
		_ = binary.Write(writer, binary.LittleEndian, value)
	}

	// Смещение для хранения дополнительных данных тега BitsPerSample (так как там 3 значения)
	bitsOffset := ifdOffset + 2 + (uint32(numTags) * 12) + 4

	// Записываем стандартные TIFF-теги
	writeTag(0x0100, 3, 1, uint32(img.Width))       // Width (Short)
	writeTag(0x0101, 3, 1, uint32(img.Height))      // Height (Short)
	writeTag(0x0102, 3, 3, bitsOffset)              // BitsPerSample (Указывает на массив)
	writeTag(0x0103, 3, 1, 1)                       // Compression: 1 = No Compression
	writeTag(0x0106, 3, 1, 2)                       // Photometric Interpretation: 2 = RGB
	writeTag(0x0111, 4, 1, 8)                       // StripOffsets: данные начинаются с 8-го байта
	writeTag(0x0115, 3, 1, 3)                       // SamplesPerPixel: 3 канала
	writeTag(0x0116, 4, 1, uint32(img.Height))      // RowsPerStrip: высота кадра
	writeTag(0x0117, 4, 1, uint32(len(img.Data)*2)) // StripByteCounts: размер блока пикселей в байтах
	writeTag(0x011C, 3, 1, 1)                       // PlanarConfiguration: 1 = Chunky (Interleaved)

	// Смещение до следующего IFD (0 = конец файла)
	var nextIFD uint32 = 0
	_ = binary.Write(writer, binary.LittleEndian, nextIFD)

	// Записываем сами значения для тега BitsPerSample [16, 16, 16] (канал R, G, B)
	_ = binary.Write(writer, binary.LittleEndian, uint16(16))
	_ = binary.Write(writer, binary.LittleEndian, uint16(16))
	_ = binary.Write(writer, binary.LittleEndian, uint16(16))

	// Финальный сброс буфера на диск
	err = writer.Flush()
	if err != nil {
		return fmt.Errorf("ошибка финализации файла: %w", err)
	}

	return nil
}
