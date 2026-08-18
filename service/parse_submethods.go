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
	"errors"
	"fmt"
	"unsafe"
)

// initAndUnpackLibRaw инициализирует контекст LibRaw, открывает указанный файл и распаковывает его RAW-матрицу.
// Внимание: вызывающий код обязан выполнить defer C.libraw_close(ipdata) для предотвращения утечек памяти.
/*
Он отвечает за выделение памяти в Си, открытие RAF-файла и его распаковку в оперативную память.

Что делает этот кусок кода:
Вызывает оригинальный сишный libraw_init.
Безопасно аллоцирует строку пути в памяти Си через C.CString и гарантирует её очистку через defer C.free.
Открывает и распаковывает файл (libraw_open_file и libraw_unpack). Если на каком-то этапе происходит сбой,
он корректно закрывает дескриптор Си через C.libraw_close, чтобы не плодить зомби-процессы в памяти, и возвращает ошибку Go.
*/
func initAndUnpackLibRaw(filePath string) (*C.libraw_data_t, error) {
	ipdata := C.libraw_init(0)
	if ipdata == nil {
		return nil, errors.New("не удалось инициализировать контекст LibRaw")
	}

	cPath := C.CString(filePath)
	defer C.free(unsafe.Pointer(cPath))

	if ret := C.libraw_open_file(ipdata, cPath); ret != 0 {
		C.libraw_close(ipdata)
		return nil, fmt.Errorf("libraw не смог открыть файл (код ошибки: %d)", ret)
	}

	if ret := C.libraw_unpack(ipdata); ret != 0 {
		C.libraw_close(ipdata)
		return nil, fmt.Errorf("ошибка распаковки матрицы LibRaw (код ошибки: %d)", ret)
	}

	return ipdata, nil
}

// configureLibRawParams задает внутренние флаги LibRaw для точного управления цветом,
// экспозицией и алгоритмами обработки пересветов.
/*
Он управляет поведением движка LibRaw. Чтобы мы могли использовать эту функцию для обоих проходов (честного и восстановительного),
мы передаем в нее режим обработки светов (highlightMode) и поправку экспозиции (exposure) [LibRaw].

Что делает этот код:
Фиксирует настройки: устанавливает 16 бит, sRGB, баланс белого камеры и отключает авто-яркость [LibRaw].
Задает геометрию: half_size = 1 гарантирует работу в одинаковом разрешении для обоих проходов [LibRaw].
Управляет светами: задает режим (highlightMode) и коррекцию (exposure), позволяя переключаться между честным цветом и восстановлением [LibRaw].
*/
func configureLibRawParams(ipdata *C.libraw_data_t, highlightMode int, exposureShift float64) {

	ipdata.params.use_camera_wb = 1 // Родной баланс белого камеры

	// Базовые параметры: 16 бит, белый баланс камеры, sRGB, без авто-яркости
	ipdata.params.output_bps = 16
	ipdata.params.use_camera_wb = 1
	ipdata.params.output_color = 1
	ipdata.params.no_auto_bright = 1

	// half_size = 1 включает аппаратный биннинг 2x2, гарантируя одинаковую геометрию для обоих проходов
	ipdata.params.half_size = 1

	// Динамические параметры: 0 = жесткий клиппинг, 2 = метод Blend
	ipdata.params.highlight = C.int(highlightMode)
	// ipdata.params.highlight = 1 // Чистый белый глянец бликов

	// Настройка коррекции экспозиции по спецификации LibRaw
	if exposureShift != 1.0 {
		ipdata.params.exp_correc = 1
		ipdata.params.exp_shift = C.float(exposureShift) // Линейный множитель (например, 0.25 для -2 EV)
		ipdata.params.exp_preser = C.float(0.0)          // Выключаем встроенное сжатие светов, блендим вручную
	} else {
		ipdata.params.exp_correc = 0
		ipdata.params.exp_shift = C.float(1.0)
		ipdata.params.exp_preser = C.float(0.0)
	}
}

// extractPixelsFromC конвертирует внутренний 4-канальный Си-массив LibRaw
// в стандартный трехканальный плоский Go-слайс []uint16 (RGB).
/*
Он отвечает за перенос данных из структуры памяти Си (ipdata.image) в чистый и производительный плоский массив Go []uint16 [LibRaw].
В процессе переноса мы отбрасываем четвертый (служебный) канал Си-структуры и оставляем классическую трехканальную RGB-структуру кадра [LibRaw].

Что делает этот код:
unsafe.Slice: Безопасно сопоставляет область памяти Си со слайсом Go без выделения дополнительной памяти на этом этапе.
Фильтрация каналов: Проходит по всему изображению, забирая только полезные значения Red, Green и Blue. Четвертый канал игнорируется.
Совместимость: На выходе отдает точно такой же плоский []uint16 массив, который используется во всем остальном пайплайне вашего парсера rafparser.
*/
func extractPixelsFromC(ipdata *C.libraw_data_t, width, height int) []uint16 {
	// В памяти Си каждый пиксель представлен 4-мя элементами ushort (R, G, B, G2)
	cArrayLength := width * height * 4
	cPixels := unsafe.Slice((*C.ushort)(unsafe.Pointer(ipdata.image)), cArrayLength)

	// В выходном Go-массиве нам нужны строго 3 канала (R, G, B)
	totalElements := width * height * 3
	pixelData := make([]uint16, totalElements)

	idx := 0
	for i := 0; i < width*height; i++ {
		cIdx := i * 4
		pixelData[idx] = uint16(cPixels[cIdx])     // Red
		pixelData[idx+1] = uint16(cPixels[cIdx+1]) // Green
		pixelData[idx+2] = uint16(cPixels[cIdx+2]) // Blue
		idx += 3
	}

	return pixelData
}

// blendHighlights производит попиксельное сшивание нормального и недосвеченного кадров.
// threshold — значение яркости (например, 55000), выше которого начинается плавное подмешивание светов.
/*
Он выполняет попиксельный блендинг (сшивание) двух проявленных массивов пикселей [LibRaw].
Если пиксель в нормальном кадре (pureData) находится в безопасной зоне (ниже порога threshold), мы берем его на 100% без изменений [LibRaw].
Если же пиксель приближается к пересвету, код плавно замещает его детализированными данными из недосвеченного кадра (darkData), компенсируя падение яркости [LibRaw].

Что делает этот код:
Экономия ресурсов: На всех пикселях, которые не доходят до пересвета, отрабатывает быстрый continue [LibRaw]. Это сохраняет 100% честность вашего биннинга для теней и полутонов [LibRaw].
Линейная интерполяция (LERP): В зоне пересвета переход происходит плавно (по фактору alpha), что полностью исключает появление резких рваных границ и бирюзовых артефактов на стыке нормального кадра и дыры.
Восстановление светимости (gain): Поскольку темный кадр снимался с экспозицией -2.0 EV, его пиксели умножаются на 4.0, чтобы они встали на свое законное место по яркости, но при этом принесли с собой невыбитые детали текстуры.
*/
func blendHighlights(pureData, darkData []uint16, threshold uint16) []uint16 {
	finalData := make([]uint16, len(pureData))

	// Коэффициент компенсации экспозиции для темного кадра (-2.0 EV означает, что данные темнее в 4 раза)
	// Умножение на 4.0 возвращает пиксели темного кадра к исходной физической яркости
	const gain float64 = 4.0
	const maxVal float64 = 65535.0

	thresholdF := float64(threshold)
	// Зона блендинга — расстояние от порога до абсолютного потолка 16 бит
	blendRange := maxVal - thresholdF

	for i := 0; i < len(pureData); i++ {
		pureVal := float64(pureData[i])

		// Если значение пикселя ниже порога — берем честный биннинг на 100%
		if pureVal < thresholdF {
			finalData[i] = pureData[i]
			continue
		}

		// Вычисляем фактор смешивания (LERP weight) от 0.0 (на пороге) до 1.0 (полный пересвет)
		alpha := (pureVal - thresholdF) / blendRange
		if alpha > 1.0 {
			alpha = 1.0
		}

		// Извлекаем "спасенное" значение из темного кадра и восстанавливаем его реальную яркость
		darkCompensated := float64(darkData[i]) * gain

		// Линейная интерполяция между честным пикселем и компенсированным восстановленным
		blended := (1.0-alpha)*pureVal + alpha*darkCompensated

		// Защита от выхода за границы диапазона uint16
		if blended > maxVal {
			blended = maxVal
		} else if blended < 0.0 {
			blended = 0.0
		}

		finalData[i] = uint16(blended)
	}

	return finalData
}
