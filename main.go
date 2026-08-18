package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/claygod/rafparser/domain"
	"github.com/claygod/rafparser/service"
)

func main() {
	file, err := os.Open("gfx100_test.RAF")
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
	fmt.Printf("[Превью] Извлечение JPEG (%d байт)... ", offsets.JPEGLen)
	err = service.ExtractEmbeddedJPEG(file, offsets.JPEGOffset, offsets.JPEGLen, "embedded_preview.jpg")
	if err != nil {
		fmt.Printf("❌ Ошибка: %v\n", err)
	} else {
		fmt.Println("✅ Успешно сохранено в 'embedded_preview.jpg'")
	}
	fmt.Println("==================================================")

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

	// вариант с бинингом
	// Читаем чистый, неинтерполированный Bayer из RAF
	pixelData, err := service.ReadCFAData("gfx100_test.RAF", meta)
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
	service.ApplyBlackLevelAndContrast(imageRGB, 3000, 1.05)

	// =================================================================
	// НОВЫЙ БЛОК: НАКАДРОВЫЙ ШАРПИНГ ХАЙ-ЭНД КЛАССА
	// =================================================================
	// Параметры: 1.2 = Сила 120%, 400 = Порог отсечки шума в глубоких тенях
	service.ApplyUnsharpMask16(imageRGB, 0.5, 600)

	fmt.Println(" Экспорт финального 16-битного честного TIFF...")
	err = service.ExportToTIFF16(imageRGB, "final_binning_gfx100.tif")

	// =================================================================
	// ДОПОЛНИТЕЛЬНЫЙ КУСОЧЕК: ЭКСПОРТ ЧИСТОЙ БАЙЕРОВСКОЙ МОЗАИКИ
	// =================================================================
	fmt.Println("\n[Анализ] Извлечение и визуализация чистой Bayer-решетки...")

	// 1. Выдергиваем чистые пиксели АЦП без демозаика и баланса белого
	pureBayer, err := service.ReadPureBayerData("gfx100_test.RAF", meta)
	if err != nil {
		fmt.Printf("❌ Ошибка чтения чистого Bayer: %v\n", err)
		return
	}

	// 2. Раскладываем пиксели по своим физическим RGB-каналам
	bayerMosaicImage := service.VisualizeBayerMosaic(pureBayer, int(meta.Width), int(meta.Height))

	// 3. Сохраняем эталонную цветную сетку на диск под именем bayer_mosaic_gfx100.tif
	fmt.Println("Сохранение файла bayer_mosaic_gfx100.tif на диск...")
	err = service.ExportToTIFF16(bayerMosaicImage, "bayer_mosaic_gfx100.tif")
	if err != nil {
		fmt.Printf("❌ Ошибка экспорта мозаики: %v\n", err)
		return
	}
	fmt.Println("Чистая Bayer-решетка успешно сохранена в 'bayer_mosaic_gfx100.tif'!")
	// =================================================================

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
	err = service.ExportToLinearDNG(imageRGB, "final_binning_gfx100.dng") // dngImageStruct
	if err != nil {
		fmt.Printf("❌ Ошибка экспорта в DNG: %v\n", err)
		return
	}
	fmt.Println("Базовая DNG-версия успешно воссоздана!")

}
