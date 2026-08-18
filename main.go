package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/claygod/rafparser/domain"
	"github.com/claygod/rafparser/service"
)

/*

Как передать точный уровень белого из RAF без добавления полей?Если вы вызываете этот метод в коде,
где доступен исходный контекст Си-библиотеки LibRaw (ipdata *C.libraw_data_t),
вы можете извлечь точный динамический уровень белого прямо «на лету» из его структуры:

// Считываем физический уровень клиппинга конкретного кадра из памяти LibRaw
whiteLevel := float32(ipdata.color.maximum)
// Вызываем экспортер EXR
err = service.ExportToLinearEXR(img, whiteLevel, "output.exr")

Поле ipdata.color.maximum — это официальная переменная LibRaw,
куда движок записывает вытащенный из метаданных (MakerNotes) уровень насыщения сенсора.
Например, для Fujifilm X-E2/X-T2 там обычно автоматически окажется число 15872

Речь про аргумент, который потребуется ниже: fujiWhiteLevel ~= 16000.0

*/

const exposureCompensation float32 = 3.5

func main() {
	// Вызываем наш парсер командной строки
	cmdConf, err := ParseCommandLine()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[rafparser] Исходный файл: %s\n", cmdConf.InputPath)
	if cmdConf.Recovery {
		fmt.Println("[rafparser] Режим восстановления светов: АКТИВЕН")
	}

	file, err := os.Open(cmdConf.InputPath) // "gfx100_test.RAF")
	if err != nil {
		fmt.Printf("❌ Ошибка открытия файла: %v\n", err)
		return
	}
	defer file.Close()

	_, _ = file.Seek(0, io.SeekStart)

	var header domain.RAFHeader
	if err := binary.Read(file, binary.BigEndian, &header); err != nil {
		fmt.Printf("❌ Ошибка чтения заголовка: %v\n", err)
		return
	}

	if string(header.MagicString[:]) != "FUJIFILMCCD-RAW " {
		fmt.Println("❌ Критическая ошибка: Невалидный формат RAF")
		return
	}

	var offsets domain.RAFOffsets
	if err := binary.Read(file, binary.BigEndian, &offsets); err != nil {
		fmt.Printf("❌ Ошибка чтения смещений: %v\n", err)
		return
	}

	meta, err := service.ParseRAFMetadata(file)
	if err != nil {
		fmt.Printf("❌ Ошибка парсинга метаданных: %v\n", err)
		return
	}

	// Выводим красивый паспорт кадра
	fmt.Println("\n==================================================")
	fmt.Println("       УСПЕШНЫЙ ПАРСИНГ МЕТАДАННЫХ FUJIFILM GFX     ")
	fmt.Println("==================================================")
	fmt.Printf(" Камера:            %s\n", meta.CameraModel)
	fmt.Printf(" Версия формата RAF: %s\n", meta.RAFVersion)
	fmt.Printf(" Разрешение кадра:  %d x %d пикселей\n", meta.Width, meta.Height)
	fmt.Printf(" Разрядность цвета: %d бит\n", meta.BitDepth)
	fmt.Printf(" Паттерн матрицы:   %s\n", meta.Pattern)
	fmt.Println("--------------------------------------------------")
	fmt.Println("       КАЛИБРОВОЧНЫЕ ДАННЫЕ ДЛЯ ПРОЯВКИ          ")
	fmt.Println("--------------------------------------------------")
	fmt.Printf(" Уровень черного (Black Level): %d\n", meta.BlackLevel)
	fmt.Printf(" Баланс белого (WB [R, G1, G2, B]): %v\n", meta.WhiteBalance)
	fmt.Println("--------------------------------------------------")

	// 3. ИЗВЛЕЧЕНИЕ ВСТРОЕННОГО JPEG ПРЕВЬЮ
	if cmdConf.Preview != nil {
		fmt.Printf("[Превью] Извлечение JPEG (%d байт)... ", offsets.JPEGLen)
		err = service.ExtractEmbeddedJPEG(file, offsets.JPEGOffset, offsets.JPEGLen, cmdConf.Preview.OutPath) // "embedded_preview.jpg"
		if err != nil {
			fmt.Printf("❌ Ошибка: %v\n", err)
		} else {
			fmt.Println("✅ Успешно сохранено в 'embedded_preview.jpg'")
		}
		fmt.Println("==================================================")
	}

	// 4. ЧТЕНИЕ МАТРИЦЫ ПИКСЕЛЕЙ ЧЕРЕЗ СЕРВИС (Оптимизировано)
	fmt.Println("Загрузка и калибровка пиксельной матрицы...")
	fmt.Println("\nЗагрузка и декомпрессия матрицы через LibRaw...")
	fmt.Println("\nЗагрузка и идеальная проявка через LibRaw...")

	// вариант с либой
	// pixelData, err := service.ReadCFAData("gfx100_test.RAF", meta)
	// if err != nil {
	// 	fmt.Printf("❌ Ошибка чтения: %v\n", err)
	// 	return
	// }
	// imageRGB := &domain.RGBImage16{
	// 	Width:  int(meta.Width),
	// 	Height: int(meta.Height),
	// 	Data:   pixelData,
	// }
	// fmt.Println("Экспорт финального 16-битного цветного TIFF...")
	// err = service.ExportToTIFF16(imageRGB, "final_color_gfx100.tif")

	// // вариант с бинингом
	// Читаем чистый, неинтерполированный Bayer из RAF
	// pixelData, err := service.ReadCFAData("gfx100_test.RAF", meta)
	// if err != nil {
	// 	fmt.Printf("❌ Ошибка чтения: %v\n", err)
	// 	return
	// }

	// вариант с lдвумя проходами для восстановления пересветов
	// Читаем чистый, неинтерполированный Bayer из RAF
	pixelData, err := service.ReadCFADataLibModular(cmdConf.InputPath, meta, true) // "gfx100_test.RAF"
	if err != nil {
		fmt.Printf("❌ Ошибка чтения: %v\n", err)
		return
	}

	// Упаковываем полученный массив в доменную структуру (размеры уже половинные)
	imageRGB := &domain.RGBImage16{
		Width:  int(meta.Width),
		Height: int(meta.Height),
		Data:   pixelData,
	}

	// Мягко наводим контраст в Go, удаляя остаточную серую вуаль
	//fmt.Println("Тонкая настройка точки черного и контраста...")
	//service.ApplyBlackLevelAndContrast(imageRGB, 3000, 1.05)

	// =================================================================
	// ЭКСПОРТ TIF биненного файла
	// =================================================================
	// Параметры: 1.2 = Сила 120%, 400 = Порог отсечки шума в глубоких тенях
	//service.ApplyUnsharpMask16(imageRGB, 0.5, 600)
	if cmdConf.TIF != nil {
		fmt.Println(" ЭКСПОРТ TIF биненного файла...")
		err = service.ExportToTIFF16(imageRGB, cmdConf.TIF.OutPath) //  "final_binning_gfx100.tif"
	}

	// =================================================================
	// ЭКСПОРТ EXR файла
	// =================================================================
	// Параметры: 1.2 = Сила 120%, 400 = Порог отсечки шума в глубоких тенях
	//service.ApplyUnsharpMask16(imageRGB, 0.5, 600)
	if cmdConf.EXR != nil {
		fmt.Println(" ЭКСПОРТ EXR файла...")
		err = service.ExportToLinearEXR(imageRGB, 15750.0, float32(cmdConf.EXR.Light), cmdConf.EXR.OutPath) //  exposureCompensation "final_binning_gfx100.exr"
	}
	// =================================================================
	// ЭКСПОРТ ЧИСТОЙ БАЙЕРОВСКОЙ МОЗАИКИ
	// =================================================================
	if cmdConf.Mosaic != nil {
		fmt.Println("\n[Анализ] Извлечение и визуализация чистой Bayer-решетки...")

		// 1. Выдергиваем чистые пиксели АЦП без демозаика и баланса белого
		pureBayer, err := service.ReadPureBayerData(cmdConf.InputPath, meta) // "gfx100_test.RAF"
		if err != nil {
			fmt.Printf("❌ Ошибка чтения чистого Bayer: %v\n", err)
			return
		}

		// 2. Раскладываем пиксели по своим физическим RGB-каналам
		bayerMosaicImage := service.VisualizeBayerMosaic(pureBayer, int(meta.Width), int(meta.Height))

		// 3. Сохраняем эталонную цветную сетку на диск под именем bayer_mosaic_gfx100.tif
		fmt.Println("Сохранение файла bayer_mosaic_gfx100.tif на диск...")
		err = service.ExportToTIFF16(bayerMosaicImage, cmdConf.Mosaic.OutPath) //  "bayer_mosaic_gfx100.tif"
		if err != nil {
			fmt.Printf("❌ Ошибка экспорта мозаики: %v\n", err)
			return
		}
		fmt.Println("Чистая Bayer-решетка успешно сохранена в ' mosaic ... .tif'!")
	}
	// =================================================================
	if cmdConf.DNG != nil {
		// -- добавка для DNG
		// Внутри main.go (Пайплайн 2):
		fmt.Println("\n[Пайплайн 2] Извлечение полного линейного массива для Linear DNG...")

		// fullRGBData, err := service.ReadPureBayerData("gfx100_test.RAF", meta)
		// if err != nil {
		// 	fmt.Printf("❌ Ошибка чтения линейных данных: %v\n", err)
		// 	return
		// }

		// // Делаем биннинг силами Go
		// fmt.Println("Масштабирование кадра 2x2 и схлопывание каналов в Go...")
		// dngImageStruct := service.RGBImageBinning2x2(fullRGBData, int(meta.Width), int(meta.Height))

		// Запускаем наш базовый, открывающийся DNG-экспортер
		fmt.Println("Создание и упаковка эластичного Linear DNG негатива...")
		fmt.Println(imageRGB.Width)
		fmt.Println(imageRGB.Height)
		err = service.ExportToLinearDNG(imageRGB, cmdConf.DNG.OutPath) // dngImageStruct "final_binning_gfx100.dng"
		if err != nil {
			fmt.Printf("❌ Ошибка экспорта в DNG: %v\n", err)
			return
		}
		fmt.Println("Базовая DNG-версия успешно воссоздана!")
	}

}
