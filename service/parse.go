package service

/*
#cgo linux LDFLAGS: -lraw
#cgo darwin LDFLAGS: -L/opt/homebrew/lib -lraw
#cgo darwin CFLAGS: -I/opt/homebrew/include
#include <libraw/libraw.h>
#include <stdlib.h>
*/
import "C"

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"unsafe"

	"github.com/claygod/rafparser/domain"
)

// Функция для безопасного извлечения встроенного JPEG-файла
func ExtractEmbeddedJPEG(r io.ReadSeeker, offset uint32, length uint32, outputPath string) error {
	// Перематываем курсор на начало JPEG
	_, err := r.Seek(int64(offset), io.SeekStart)
	if err != nil {
		return fmt.Errorf("не удалось перейти к смещению JPEG: %w", err)
	}

	// Создаем выходной файл на диске
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("не удалось создать файл превью: %w", err)
	}
	defer outFile.Close()

	// Используем io.CopyN для эффективной потоковой передачи байт без выделения лишней памяти
	// Это гораздо чище, чем делать make([]byte, length)
	_, err = io.CopyN(outFile, r, int64(length))
	if err != nil {
		return fmt.Errorf("ошибка при копировании данных JPEG: %w", err)
	}

	return nil
}

func ParseRAFMetadata(r io.ReadSeeker) (*domain.GFXMetadata, error) {
	_, err := r.Seek(0, io.SeekStart)
	if err != nil {
		return nil, err
	}

	var header domain.RAFHeader
	if err := binary.Read(r, binary.BigEndian, &header); err != nil {
		return nil, err
	}

	meta := &domain.GFXMetadata{
		CameraModel: string(bytes.Trim(header.CameraModel[:], "\x00 ")),
		RAFVersion:  string(header.Version[:]),
	}

	var offsets domain.RAFOffsets
	if err := binary.Read(r, binary.BigEndian, &offsets); err != nil {
		return nil, err
	}

	if err := parseCFAContainer(r, offsets.CFAHeaderOff, meta); err != nil {
		return nil, err
	}

	return meta, nil
}

func parseCFAContainer(r io.ReadSeeker, offset uint32, meta *domain.GFXMetadata) error {
	_, err := r.Seek(int64(offset), io.SeekStart)
	if err != nil {
		return err
	}

	var recordCount uint32
	if err := binary.Read(r, binary.BigEndian, &recordCount); err != nil {
		return err
	}

	for i := uint32(0); i < recordCount; i++ {
		var rec domain.CFARecord
		if err := binary.Read(r, binary.BigEndian, &rec); err != nil {
			return err
		}

		tagData := make([]byte, rec.Size)
		if _, err := io.ReadFull(r, tagData); err != nil {
			return err
		}

		switch rec.TagID {
		case domain.TagGFXGeometry:
			if len(tagData) >= 4 {
				meta.Height = binary.BigEndian.Uint16(tagData[0:2])
				meta.Width = binary.BigEndian.Uint16(tagData[2:4])
			}
		case domain.TagGFXBitDepth:
			if len(tagData) >= 4 {
				meta.BitDepth = binary.BigEndian.Uint16(tagData[0:2])
				meta.CompressionType = binary.BigEndian.Uint16(tagData[2:4])
				meta.IsCompressed = meta.CompressionType == 0x0030
			}
		case domain.TagGFXBayerLayout:
			if len(tagData) >= 4 {
				meta.Pattern = decodeBayerPattern(tagData)
			}
		case domain.TagGFXMakerNote:
			if len(tagData) >= 0x5358 {
				wbSlice := tagData[0x5350:0x5358]
				meta.WhiteBalance[0] = binary.LittleEndian.Uint16(wbSlice[0:2]) // R
				meta.WhiteBalance[1] = binary.LittleEndian.Uint16(wbSlice[2:4]) // G1
				meta.WhiteBalance[2] = binary.LittleEndian.Uint16(wbSlice[4:6]) // G2
				meta.WhiteBalance[3] = binary.LittleEndian.Uint16(wbSlice[6:8]) // B

				// Выполняем нормализацию относительно G1 (индекс 1)
				// Предотвращаем деление на ноль, если файл поврежден
				g1Base := float32(meta.WhiteBalance[1])
				if g1Base > 0 {
					meta.WBFactors.R = float32(meta.WhiteBalance[0]) / g1Base
					meta.WBFactors.G1 = 1.0 // База
					meta.WBFactors.G2 = float32(meta.WhiteBalance[2]) / g1Base
					meta.WBFactors.B = float32(meta.WhiteBalance[3]) / g1Base
				}
			}
			if len(tagData) >= 0x535E {
				blackSlice := tagData[0x5356:0x535E]
				meta.BlackLevel = binary.BigEndian.Uint16(blackSlice[0:2])
			}

		}
	}
	return nil
}

func decodeBayerPattern(data []byte) domain.BayerPattern {
	if len(data) < 4 {
		return domain.PatternUnknown
	}
	if data[1] == 0x07 && data[3] == 0x08 {
		return domain.PatternGRBG
	}
	return domain.PatternRGGB
}

func ReadCFAData(filePath string, meta *domain.GFXMetadata) ([]uint16, error) {
	ipdata := C.libraw_init(0)
	if ipdata == nil {
		return nil, errors.New("не удалось инициализировать контекст LibRaw")
	}
	defer C.libraw_close(ipdata)

	cPath := C.CString(filePath)
	defer C.free(unsafe.Pointer(cPath))

	if ret := C.libraw_open_file(ipdata, cPath); ret != 0 {
		return nil, fmt.Errorf("libraw не смог открыть файл (код ошибки: %d)", ret)
	}

	if ret := C.libraw_unpack(ipdata); ret != 0 {
		return nil, fmt.Errorf("ошибка распаковки матрицы LibRaw (код ошибки: %d)", ret)
	}

	// =================================================================
	// ВКЛЮЧАЕМ АППАРАТНЫЙ БИННИНГ 2x2 ВНУТРИ LIBRAW
	// =================================================================
	ipdata.params.output_bps = 16    // Честные 16 бит на канал
	ipdata.params.use_camera_wb = 1  // Родной баланс белого камеры
	ipdata.params.output_color = 4   // Стандартное цветовое пространство sRGB
	ipdata.params.no_auto_bright = 1 // Блокируем автоматический пересвет
	ipdata.params.highlight = 1      // Чистый белый глянец бликов

	// КРИТИЧЕСКИЙ ФЛАГ: half_size = 1 принудительно включает честный биннинг 2x2.
	// Движок сам схлопнет ячейки GRBG в честный RGB без демозаика!
	ipdata.params.half_size = 1

	// Мягко поднимем яркость, чтобы вернуть сочность апельсинам
	ipdata.params.bright = C.float(0.85)
	// =================================================================

	if ret := C.libraw_dcraw_process(ipdata); ret != 0 {
		return nil, fmt.Errorf("ошибка обработки цвета LibRaw (код: %d)", ret)
	}

	// Берем размеры выходного кадра (они будут ровно в 2 раза меньше исходных!)
	widthInt := int(ipdata.sizes.width)
	heightInt := int(ipdata.sizes.height)

	meta.Width = uint16(widthInt)
	meta.Height = uint16(heightInt)

	fmt.Printf("[LibRaw Биннинг] Кадр успешно проявлен! Честное RGB разрешение: %dx%d\n", widthInt, heightInt)

	// В трехканальном RGB массиве количество элементов равно: Ширина * Высота * 3
	cArrayLength := widthInt * heightInt * 4
	cPixels := unsafe.Slice((*C.ushort)(unsafe.Pointer(ipdata.image)), cArrayLength)

	totalElements := widthInt * heightInt * 3
	pixelData := make([]uint16, totalElements)

	idx := 0
	for i := 0; i < widthInt*heightInt; i++ {
		pixelData[idx] = uint16(cPixels[i*4])     // Red
		pixelData[idx+1] = uint16(cPixels[i*4+1]) // Green
		pixelData[idx+2] = uint16(cPixels[i*4+2]) // Blue
		idx += 3
	}

	return pixelData, nil
}

func ReadCFADataLib(filePath string, meta *domain.GFXMetadata) ([]uint16, error) {
	ipdata := C.libraw_init(0)
	if ipdata == nil {
		return nil, errors.New("не удалось инициализировать контекст LibRaw")
	}
	defer C.libraw_close(ipdata)

	cPath := C.CString(filePath)
	defer C.free(unsafe.Pointer(cPath))

	if ret := C.libraw_open_file(ipdata, cPath); ret != 0 {
		return nil, fmt.Errorf("libraw не смог открыть файл (код ошибки: %d)", ret)
	}

	if ret := C.libraw_unpack(ipdata); ret != 0 {
		return nil, fmt.Errorf("ошибка распаковки матрицы LibRaw (код ошибки: %d)", ret)
	}

	// // Оставляем только базовые, железно работающие флаги проявщика LibRaw
	ipdata.params.output_bps = 16    // Честные 16 бит на канал
	ipdata.params.use_camera_wb = 1  // Родной баланс белого камеры
	ipdata.params.user_qual = 3      // Высококачественный демозаик AHD
	ipdata.params.output_color = 1   // Стандартное цветовое пространство sRGB
	ipdata.params.no_auto_bright = 1 // Блокируем автоматический пересвет
	ipdata.params.highlight = 0      // Чистый белый глянец бликов

	// =================================================================
	// ПРОФЕССИОНАЛЬНАЯ НАСТРОЙКА ЦВЕТА И ЭКСПОЗИЦИИ (HIGHLIGHT RECOVERY)
	// хорошо но с пересветами
	// =================================================================
	// ipdata.params.output_bps = 16   // Честные 16 бит на канал
	// ipdata.params.use_camera_wb = 1 // Родной баланс белого камеры [1 2 2 4]
	// ipdata.params.user_qual = 3     // Высококачественный демозаик AHD
	// ipdata.params.output_color = 1  // Стандартное цветовое пространство sRGB

	// // Восстановление пересветов: 2 = метод Blend (смешивание каналов для проявления текстуры)
	// ipdata.params.highlight = 2

	// // Отключаем автоматический подъем яркости, чтобы избавиться от белых пятен на чашке
	// ipdata.params.no_auto_bright = 1

	// // Задаем идеальную промышленную гамма-кривую стандарта sRGB
	// ipdata.params.gamm[0] = 1.0 / 2.4
	// ipdata.params.gamm[1] = 12.92
	// // =================================================================

	// =================================================================
	// ИСПРАВЛЕНИЕ НАСТРОЙКИ ГАММЫ ДЛЯ CGO
	// без пересветов но с бирюзой
	// =================================================================
	// ipdata.params.output_bps = 16    // Честные 16 бит на канал
	// ipdata.params.use_camera_wb = 1  // Родной баланс белого камеры
	// ipdata.params.user_qual = 3      // Высококачественный демозаик AHD
	// ipdata.params.output_color = 1   // Стандартное цветовое пространство sRGB
	// ipdata.params.highlight = 3      // Умная пространственная реконструкция светов
	// ipdata.params.no_auto_bright = 1 // Отключаем автоматический подъем яркости

	// // ИСПРАВЛЕНО: gamm — это массив в Си. Задаем параметры sRGB по индексам:
	// ipdata.params.gamm[0] = C.double(1.0 / 2.4) // Индекс 0: Наклон инверсной гаммы
	// ipdata.params.gamm[1] = C.double(12.92)     // Индекс 1: Линейная отсечка в глубоких тенях
	// =================================================================

	if ret := C.libraw_dcraw_process(ipdata); ret != 0 {
		return nil, fmt.Errorf("ошибка обработки цвета LibRaw (код: %d)", ret)
	}

	widthInt := int(ipdata.sizes.width)
	heightInt := int(ipdata.sizes.height)
	meta.Width = uint16(widthInt)
	meta.Height = uint16(heightInt)

	cArrayLength := widthInt * heightInt * 4
	cPixels := unsafe.Slice((*C.ushort)(unsafe.Pointer(ipdata.image)), cArrayLength)

	totalElements := widthInt * heightInt * 3
	pixelData := make([]uint16, totalElements)

	idx := 0
	for i := 0; i < widthInt*heightInt; i++ {
		pixelData[idx] = uint16(cPixels[i*4])     // Red
		pixelData[idx+1] = uint16(cPixels[i*4+1]) // Green
		pixelData[idx+2] = uint16(cPixels[i*4+2]) // Blue
		idx += 3
	}

	return pixelData, nil
}

// ReadCFAData считывает сырые данные матрицы в производительный плоский массив []uint16,
// предварительно выполняя валидацию физического размера и вычитание уровня черного (Debiasing).
func ReadCFAData2221(r io.ReadSeeker, offsets domain.RAFOffsets, meta *domain.GFXMetadata) ([]uint16, error) {
	// 1. Проверяем физический размер блока данных
	expectedDataLen := int64(meta.Width) * int64(meta.Height) * 2 // Ширина * Высота * 2 байта
	actualDataLen := int64(offsets.CFADataLen)

	if actualDataLen < expectedDataLen {
		return nil, fmt.Errorf("файл физически остается сжатым (ожидалось %d байт, в файле %d байт)", expectedDataLen, actualDataLen)
	}

	// 2. Переходим к началу RAW-данных
	_, err := r.Seek(int64(offsets.CFADataOff), io.SeekStart)
	if err != nil {
		return nil, fmt.Errorf("не удалось перейти к смещению CFA данных: %w", err)
	}

	// Инициализируем буферизированный поток (размер буфера 1 МБ для минимизации сисколлов)
	reader := bufio.NewReaderSize(r, 1024*1024)

	// // 3. Выделяем память под ОДИН плоский массив (одна аллокация в куче вместо 8736)
	// totalPixels := int(meta.Width) * int(meta.Height)
	// pixelData := make([]uint16, totalPixels)

	// // Временный буфер для чтения одной строки (ширина кадра * 2 байта)
	// rowBytesBuffer := make([]byte, meta.Width*2)
	// widthInt := int(meta.Width)

	// // 4. Построчное чтение и распаковка BigEndian + Debiasing
	// for y := 0; y < int(meta.Height); y++ {
	// 	_, err := io.ReadFull(reader, rowBytesBuffer)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("ошибка при чтении строки %d: %w", y, err)
	// 	}

	// 	// Вычисляем базовое смещение для текущей строки в плоском массиве
	// 	rowOffset := y * widthInt

	// 	for x := 0; x < widthInt; x++ {
	// 		idx := x * 2
	// 		rawPixel := binary.BigEndian.Uint16(rowBytesBuffer[idx : idx+2])

	// 		// Вычитаем уровень черного с защитой от переполнения uint16
	// 		var cleanPixel uint16
	// 		if rawPixel > meta.BlackLevel {
	// 			cleanPixel = rawPixel - meta.BlackLevel
	// 		} // Иначе остается 0 (чистый черный)

	// 		// Записываем пиксель в плоский массив
	// 		pixelData[rowOffset+x] = cleanPixel
	// 	}
	// }

	// return pixelData, nil

	// Вместо фиксированного meta.Height, считаем сколько строк реально уложилось в размер файла offsets.CFADataLen
	// Временный буфер для чтения одной строки (ширина кадра * 2 байта)
	rowBytesBuffer := make([]byte, meta.Width*2)
	widthInt := int(meta.Width)

	maxAvailableRows := int(offsets.CFADataLen) / (widthInt * 2)
	if maxAvailableRows > int(meta.Height) {
		maxAvailableRows = int(meta.Height)
	}

	// Выделяем память строго под доступный объем
	totalPixels := widthInt * maxAvailableRows
	pixelData := make([]uint16, totalPixels)

	for y := 0; y < maxAvailableRows; y++ {
		_, err := io.ReadFull(reader, rowBytesBuffer)
		if err != nil {
			// Если файл внезапно кончился, просто прекращаем чтение, отдавая то, что успели
			return pixelData[:y*widthInt], nil
		}

		rowOffset := y * widthInt
		for x := 0; x < widthInt; x++ {
			idx := x * 2
			rawPixel := binary.BigEndian.Uint16(rowBytesBuffer[idx : idx+2])

			var cleanPixel uint16
			if rawPixel > meta.BlackLevel {
				cleanPixel = rawPixel - meta.BlackLevel
			}
			pixelData[rowOffset+x] = cleanPixel
		}
	}

	// Обновляем метаданные реальной прочитанной высотой, чтобы конвертер не читал "воздух"
	meta.Height = uint16(maxAvailableRows)

	return pixelData, nil

}

func ReadPureBayerData(filePath string, meta *domain.GFXMetadata) ([]uint16, error) {
	ipdata := C.libraw_init(0)
	if ipdata == nil {
		return nil, errors.New("не удалось инициализировать LibRaw")
	}
	defer C.libraw_close(ipdata)

	cPath := C.CString(filePath)
	defer C.free(unsafe.Pointer(cPath))

	if ret := C.libraw_open_file(ipdata, cPath); ret != 0 {
		return nil, fmt.Errorf("ошибка открытия файла: %d", ret)
	}

	if ret := C.libraw_unpack(ipdata); ret != 0 {
		return nil, fmt.Errorf("ошибка распаковки: %d", ret)
	}

	// ТОТ САМЫЙ РАБОЧИЙ РЕЖИМ: ПОЛНЫЙ КАДР БЕЗ НАКЛОНА СТРОК
	ipdata.params.output_bps = 16    // 16 бит
	ipdata.params.use_camera_wb = 1  // Родной БВ
	ipdata.params.user_qual = 0      // Быстрый билинейный демозаик
	ipdata.params.output_color = 1   // Выгружаем в sRGB
	ipdata.params.no_auto_bright = 1 // Блокируем авто-яркость
	ipdata.params.highlight = 0      // Клиппинг бликов
	ipdata.params.half_size = 0      // Строго 0 на стороне Си!

	if ret := C.libraw_dcraw_process(ipdata); ret != 0 {
		return nil, fmt.Errorf("ошибка обработки цвета LibRaw (код: %d)", ret)
	}

	widthInt := int(ipdata.sizes.width)
	heightInt := int(ipdata.sizes.height)

	meta.Width = uint16(widthInt)
	meta.Height = uint16(heightInt)

	cArrayLength := widthInt * heightInt * 4
	cPixels := unsafe.Slice((*C.ushort)(unsafe.Pointer(ipdata.image)), cArrayLength)

	totalElements := widthInt * heightInt * 3
	pixelData := make([]uint16, totalElements)

	idx := 0
	for i := 0; i < widthInt*heightInt; i++ {
		cIdx := i * 4
		pixelData[idx] = uint16(cPixels[cIdx])     // R
		pixelData[idx+1] = uint16(cPixels[cIdx+1]) // G
		pixelData[idx+2] = uint16(cPixels[cIdx+2]) // B
		idx += 3
	}

	return pixelData, nil
}

func ReadPureBayerData222(filePath string, meta *domain.GFXMetadata) ([]uint16, error) {
	ipdata := C.libraw_init(0)
	if ipdata == nil {
		return nil, errors.New("не удалось инициализировать LibRaw")
	}
	defer C.libraw_close(ipdata)

	cPath := C.CString(filePath)
	defer C.free(unsafe.Pointer(cPath))

	if ret := C.libraw_open_file(ipdata, cPath); ret != 0 {
		return nil, fmt.Errorf("ошибка открытия файла: %d", ret)
	}

	if ret := C.libraw_unpack(ipdata); ret != 0 {
		return nil, fmt.Errorf("ошибка распаковки: %d", ret)
	}

	// 1. ПОЛУЧАЕМ ПАРАМЕТРЫ ГЕОМЕТРИИ СЕНСОРА И СМЕЩЕНИЙ КРОПА
	stride := int(ipdata.rawdata.sizes.raw_width) // Полная физическая ширина строки (например, 11648)

	activeWidth := int(ipdata.sizes.width)   // Активная (видимая) ширина кадра
	activeHeight := int(ipdata.sizes.height) // Активная (видимая) высота кадра

	// Точные смещения, где заканчивается черная рамка и начинается реальная чашка!
	topMargin := int(ipdata.rawdata.sizes.top_margin)
	leftMargin := int(ipdata.rawdata.sizes.left_margin)

	meta.Width = uint16(activeWidth)
	meta.Height = uint16(activeHeight)
	meta.BlackLevel = uint16(ipdata.rawdata.color.black) // Уровень черного АЦП

	// 2. ДИНАМИЧЕСКИЙ СЛАЙС К ЧИСТОМУ МАССИВУ В ПАМЯТИ СИ
	cArrayLength := stride * int(ipdata.rawdata.sizes.raw_height)
	cPixels := unsafe.Slice((*C.ushort)(unsafe.Pointer(ipdata.rawdata.raw_image)), cArrayLength)

	// Выделяем память под чистый Bayer-массив строго активной области в Go
	pureActiveBayer := make([]uint16, activeWidth*activeHeight)

	// 3. КОПИРУЕМ ПИКСЕЛИ С УЧЕТОМ СМЕЩЕНИЯ ТЕХНИЧЕСКИХ ПОЛЕЙ
	for y := 0; y < activeHeight; y++ {
		// Физический индекс начала строки полезного кадра в памяти Си (с учетом top_margin и stride)
		srcRowOffset := (topMargin+y)*stride + leftMargin
		dstRowOffset := y * activeWidth

		for x := 0; x < activeWidth; x++ {
			// Забираем чистый, нетронутый пиксель АЦП без баланса белого и без sRGB
			pureActiveBayer[dstRowOffset+x] = uint16(cPixels[srcRowOffset+x])
		}
	}

	fmt.Printf("[Bayer] Выдернут чистый Bayer-массив активной зоны! Геометрия: %dx%d\n", activeWidth, activeHeight)
	return pureActiveBayer, nil
}
