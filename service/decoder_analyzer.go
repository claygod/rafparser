package service

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/claygod/rafparser/domain"
)

// AnalyzeAndDecipherRAF сравнивает эталонный TIFF из RawTherapee с сырым блоком RAF,
// чтобы вычислить точный шаг, паддинг и метод упаковки байт.
func AnalyzeAndDecipherRAF(rafPath string, tiffPath string, offsets domain.RAFOffsets) error {
	// 1. Открываем эталонный TIFF от RawTherapee
	tFile, err := os.Open(tiffPath)
	if err != nil {
		return fmt.Errorf("не удалось открыть эталонный TIFF: %w", err)
	}
	defer tFile.Close()

	// Вычисляем смещение пикселей в TIFF (отсекаем заголовки)
	width := 11648
	tiffPixelOffset := 611000664 - (width * 8736 * 3 * 2)

	_, _ = tFile.Seek(int64(tiffPixelOffset), io.SeekStart)
	tiffReader := io.Reader(tFile)

	// Читаем первую строку из TIFF (нам нужны первые 20 пикселей)
	// В TIFF на один пиксель идет 3 канала по 2 байта = 6 байт на пиксель
	tiffRowBuffer := make([]byte, width*6)
	_, _ = io.ReadFull(tiffReader, tiffRowBuffer)

	// Извлекаем первые 20 реальных (ненулевых) значений яркости из Bayer-сетки TIFF
	var targetValues []uint16
	for i := 0; i < 20; i++ {
		idx := i * 6
		r := binary.LittleEndian.Uint16(tiffRowBuffer[idx : idx+2])
		g := binary.LittleEndian.Uint16(tiffRowBuffer[idx+2 : idx+4])
		// Так как строка 0 имеет паттерн G-R-G-R, берем ненулевые каналы
		if i&1 == 0 {
			targetValues = append(targetValues, g) // Четный пиксель — Green
		} else {
			targetValues = append(targetValues, r) // Нечетный пиксель — Red
		}
	}

	fmt.Println("\n==================================================")
	fmt.Println("   АНАЛИЗ ЭТАЛОННЫХ ЗНАЧЕНИЙ ЯРКОСТИ ИЗ TIFF      ")
	fmt.Println("==================================================")
	fmt.Printf("Первые 10 Bayer-пикселей из RawTherapee: %v\n", targetValues[:10])
	fmt.Println("==================================================")

	// 2. Открываем оригинальный RAF-файл для поиска этих чисел
	rFile, err := os.Open(rafPath)
	if err != nil {
		return err
	}
	defer rFile.Close()

	// Переходим к началу встроенного в RAF массива (пропускаем заголовок контейнера TIFF в 7808 байт)
	rafStartPixels := offsets.CFADataOff + 7808
	_, _ = rFile.Seek(int64(rafStartPixels), io.SeekStart)

	// Читаем первые 60 000 байт оригинального массива RAF (хватит на пару строк)
	rafBuffer := make([]byte, 60000)
	_, _ = io.ReadFull(rFile, rafBuffer)

	fmt.Println("\n[Поиск] Сканирование бинарной структуры RAF...")

	// ИЩЕМ НАШИ ПИКСЕЛИ В RAF ЧЕРЕЗ РАЗНЫЕ ГИПОТЕЗЫ
	// Гипотеза А: Данные идут последовательно в LittleEndian, но сдвинуты (уровень черного)
	// В TIFF уровень черного уже вычтен (обычно 1024). Значит в RAF число должно быть: TIFF_Value + 1024
	matchCountLE := 0
	for i := 0; i < len(rafBuffer)-2; i += 2 {
		valRaw := binary.LittleEndian.Uint16(rafBuffer[i : i+2])

		// Проверяем, совпадает ли первый пиксель с учетом BlackLevel = 1024
		if valRaw == targetValues[0]+1024 {
			fmt.Printf("Найдено точное совпадение (LittleEndian) на байтовом смещении: %d (Значение в RAF: %d)\n", i, valRaw)
			matchCountLE++
			if matchCountLE > 3 {
				break
			}
		}
	}

	// Гипотеза Б: Данные в RAF идут в BigEndian
	matchCountBE := 0
	for i := 0; i < len(rafBuffer)-2; i += 2 {
		valRaw := binary.BigEndian.Uint16(rafBuffer[i : i+2])
		if valRaw == targetValues[0]+1024 {
			fmt.Printf("Найдено точное совпадение (BigEndian) на байтовом смещении: %d (Значение в RAF: %d)\n", i, valRaw)
			matchCountBE++
			if matchCountBE > 3 {
				break
			}
		}
	}

	// Гипотеза В: Битовая упаковка 14-бит (проверим шаг между соседними эталонами)
	fmt.Println("[Инфо] Поиск завершен. Если совпадений нет, мы выведем сырой дамп первых байт RAF для ручного анализа.")
	if matchCountLE == 0 && matchCountBE == 0 {
		fmt.Printf("Сырые первые 16 байт блока RAF (Hex): %X\n", rafBuffer[:16])
	}

	return nil
}
