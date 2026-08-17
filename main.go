package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// Фиксированный заголовок RAF-файла Fujifilm (размер: 84 байта)
type RAFHeader struct {
	MagicString    [16]byte // "FUJIFILMCCD-RAW "
	Version        [4]byte  // Например, "0201"
	CameraID       [8]byte  // Внутренний ID камеры
	CameraModel    [32]byte // Название модели (например, "X-T200  ")
	DirVersion     [4]byte  // Версия директории (например, "0100")
	Reserved       [20]byte // Зарезервировано / Неизвестно
}

// Директория смещений (Offsets Directory), которая идет сразу за заголовком
type RAFOffsets struct {
	JPEGOffset     uint32 // Смещение до встроенного JPEG
	JPEGLen        uint32 // Длина встроенного JPEG
	CFAHeaderOff   uint32 // Смещение до метаданных геометрии сенсора
	CFAHeaderLen   uint32 // Длина метаданных
	CFADataOff     uint32 // Начало сырых RAW-данных
	CFADataLen     uint32 // Длина RAW-данных
}

func main() {
	// 1. Открываем RAF-файл
	file, err := os.Open("example_bayer.RAF")
	if err != nil {
		fmt.Printf("Ошибка открытия файла: %v\n", err)
		return
	}
	defer file.Close()

	// 2. Читаем заголовок (Всегда BigEndian!)
	var header RAFHeader
	err = binary.Read(file, binary.BigEndian, &header)
	if err != nil {
		fmt.Printf("Ошибка чтения заголовка: %v\n", err)
		return
	}

	// Проверяем сигнатуру
	magic := string(header.MagicString[:])
	if magic != "FUJIFILMCCD-RAW " {
		fmt.Println("Критическая ошибка: Это не валидный Fujifilm RAF-файл")
		return
	}

	// Выводим базовую информацию о камере
	fmt.Printf("--- Информация о файле ---\n")
	fmt.Printf("Камера: %s\n", string(bytes.Trim(header.CameraModel[:], "\x00 ")))
	fmt.Printf("Версия RAF: %s\n", string(header.Version[:]))

	// 3. Читаем смещения
	var offsets RAFOffsets
	err = binary.Read(file, binary.BigEndian, &offsets)
	if err != nil {
		fmt.Printf("Ошибка чтения таблицы смещений: %v\n", err)
		return
	}

	fmt.Printf("\n--- Смещения (Offsets) ---\n")
	fmt.Printf("Встроенный JPEG: Смещение 0x%X, Длина %d байт\n", offsets.JPEGOffset, offsets.JPEGLen)
	fmt.Printf("Метаданные CFA:  Смещение 0x%X, Длина %d байт\n", offsets.CFAHeaderOff, offsets.CFAHeaderLen)
	fmt.Printf("Данные матрицы:  Смещение 0x%X, Длина %d байт\n", offsets.CFADataOff, offsets.CFADataLen)

	// 4. Демонстрация извлечения встроенного JPEG
	err = extractEmbeddedJPEG(file, offsets.JPEGOffset, offsets.JPEGLen, "preview.jpg")
	if err != nil {
		fmt.Printf("Не удалось извлечь JPEG: %v\n", err)
	} else {
		fmt.Println("\n[Успех] Встроенный JPEG успешно сохранен в 'preview.jpg'")
	}
}

// Функция для быстрого сброса встроенного JPEG на диск
func extractEmbeddedJPEG(rs io.ReadSeeker, offset uint32, length uint32, outputPath string) error {
	_, err := rs.Seek(int64(offset), io.SeekStart)
	if err != nil {
		return err
	}

	outBuf := make([]byte, length)
	_, err = io.ReadFull(rs, outBuf)
	if err != nil {
		return err
	}

	return os.WriteFile(outputPath, outBuf, 0644)
}
