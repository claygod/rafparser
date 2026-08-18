package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Опции для конкретных форматов экспорта
type DNGConfig struct{ OutPath string }
type EXRConfig struct {
	OutPath string
	Light   float64
}
type MosaicConfig struct{ OutPath string }
type TIFConfig struct{ OutPath string }
type PreviewConfig struct{ OutPath string }

// CmdConfig — итоговая структура конфигурации, содержащая только валидные данные
type CmdConfig struct {
	InputPath string
	Recovery  bool
	DNG       *DNGConfig
	EXR       *EXRConfig
	Mosaic    *MosaicConfig
	TIF       *TIFConfig
	Preview   *PreviewConfig
}

// ParseCommandLine парсит аргументы, требуя, чтобы ВХОДНОЙ ФАЙЛ ШЕЛ ПЕРВЫМ АРГУМЕНТОМ.
func ParseCommandLine() (*CmdConfig, error) {
	// 1. ИНИЦИАЛИЗАЦИЯ И РЕГИСТРАЦИЯ ФЛАГОВ (Обязательно в самом начале для корректного вывода справки)
	var (
		flagDNG     bool
		flagEXR     bool
		flagMos     bool
		flagTIF     bool
		flagPreview bool
		recovery    bool
		light       float64
	)

	flag.BoolVar(&flagDNG, "dng", false, "Генерировать цифровой негатив (.raf.dng)")
	flag.BoolVar(&flagEXR, "exr", false, "Генерировать HDR-файл OpenEXR (.raf.exr)")
	flag.BoolVar(&flagMos, "mosaic", false, "Генерировать технический TIFF мозаики (.raf.mosaic.tif)")
	flag.BoolVar(&flagTIF, "tif", false, "Генерировать стандартный TIFF кадр (.raf.tif)")
	flag.BoolVar(&flagPreview, "preview", false, "Извлечь встроенное JPEG-превью (.raf.preview.jpg)")
	flag.BoolVar(&recovery, "recovery", false, "Включить двухпроходное восстановление светов")
	flag.Float64Var(&light, "light", 3.5, "Коэффициент ослабления яркости для EXR")

	// Переопределяем Usage для формирования красивого вывода в стиле Linux CLI
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Использование: rafparser <путь_к_файлу.raf> [опции] [папка_вывода]\n\n")
		fmt.Fprintf(os.Stderr, "Внимание:\n")
		fmt.Fprintf(os.Stderr, "  Путь к RAF файлу должен быть строго ПЕРВЫМ аргументом командной строки.\n")
		fmt.Fprintf(os.Stderr, "  Если в самый конец строки передан путь к папке (например, из IDE),\n")
		fmt.Fprintf(os.Stderr, "  все результаты будут автоматически сохранены в неё.\n\n")
		fmt.Fprintf(os.Stderr, "Опции:\n")
		flag.PrintDefaults()
	}

	// 2. ПЕРЕХВАТ И ПРОВЕРКА ПЕРВОГО АРГУМЕНТА
	if len(os.Args) < 2 {
		flag.Usage()
		return nil, fmt.Errorf("ошибка: не указан входной RAF файл")
	}

	inputPath := os.Args[1]

	// Если первым аргументом прилетел любой из флагов вызова помощи — сразу отдаем красивую справку
	if inputPath == "-h" || inputPath == "--help" || inputPath == "-help" || inputPath == "help" {
		flag.Usage()
		os.Exit(0)
	}

	// Проверяем физическое существование входного файла
	if _, err := os.Stat(inputPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("ошибка: входной файл не найден по пути: %s\n(напоминание: путь к файлу пишется строго ПЕРВЫМ аргументом)", inputPath)
	}

	// 3. ПАРСИНГ ОСТАЛЬНОЙ СТРОКИ (Флаги, идущие со 2-й позиции)
	if len(os.Args) > 2 {
		if err := flag.CommandLine.Parse(os.Args[2:]); err != nil {
			return nil, err
		}
	}

	// 4. КРОСС ПЛАТФОРМЕННЫЙ РАСЧЕТ ПУТЕЙ СОХРАНЕНИЯ (filepath)
	absInputPath, err := filepath.Abs(inputPath)
	if err != nil {
		return nil, fmt.Errorf("ошибка при определении абсолютного пути файла: %w", err)
	}

	// По умолчанию папка сохранения — рядом с исходным файлом
	targetDir := filepath.Dir(absInputPath)

	// Перехватываем путь к папке из хвоста аргументов (для совместимости с IDE)
	extraArgs := flag.Args()
	if len(extraArgs) > 0 {
		potentialDir := extraArgs[len(extraArgs)-1]
		potentialDir = strings.Trim(potentialDir, "[]")

		if stat, err := os.Stat(potentialDir); err == nil && stat.IsDir() {
			targetDir = filepath.Clean(potentialDir)
		}
	}

	baseName := filepath.Base(absInputPath)
	config := &CmdConfig{
		InputPath: absInputPath,
		Recovery:  recovery,
	}

	// 5. СБОРКА И ВАЛИДАЦИЯ СУБСТРУКТУР ЭКСПОРТА
	if flagDNG {
		config.DNG = &DNGConfig{OutPath: filepath.Join(targetDir, baseName+".dng")}
	}
	if flagEXR {
		if light < 1.0 {
			return nil, fmt.Errorf("ошибка флага '-light': значение %v не может быть меньше 1.0", light)
		}
		config.EXR = &EXRConfig{
			OutPath: filepath.Join(targetDir, baseName+".exr"),
			Light:   light,
		}
	}
	if flagMos {
		config.Mosaic = &MosaicConfig{OutPath: filepath.Join(targetDir, baseName+".mosaic.tif")}
	}
	if flagTIF {
		config.TIF = &TIFConfig{OutPath: filepath.Join(targetDir, baseName+".tif")}
	}
	if flagPreview {
		config.Preview = &PreviewConfig{OutPath: filepath.Join(targetDir, baseName+".preview.jpg")}
	}

	// Финальная валидация: выбран ли хотя бы один формат для работы?
	if config.DNG == nil && config.EXR == nil && config.Mosaic == nil && config.TIF == nil && config.Preview == nil {
		return nil, fmt.Errorf("ошибка: не выбран ни один формат для генерации (-dng, -exr, -mosaic, -tif, -preview)")
	}

	return config, nil
}
