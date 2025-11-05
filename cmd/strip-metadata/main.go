package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Поддерживаемые форматы видео
	videoExtensions = ".mp4,.mkv,.avi,.mov,.wmv,.flv,.webm,.m4v"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	workDir, err := os.Getwd()
	if err != nil {
		log.Fatalf("Не удалось получить текущую директорию: %v", err)
	}

	// Проверяем наличие ffmpeg (с автоматической установкой)
	if err := checkFFmpeg(ctx); err != nil {
		log.Fatalf("Ошибка проверки ffmpeg: %v", err)
	}

	log.Printf("Сканирование директории: %s", workDir)

	videoFiles, err := findVideoFiles(workDir)
	if err != nil {
		log.Fatalf("Ошибка поиска видеофайлов: %v", err)
	}

	if len(videoFiles) == 0 {
		log.Println("Видеофайлы не найдены")
		return
	}

	log.Printf("Найдено видеофайлов: %d", len(videoFiles))

	for i, file := range videoFiles {
		log.Printf("\n[%d/%d] Обработка: %s", i+1, len(videoFiles), filepath.Base(file))

		// Читаем метаданные до обработки
		metadata, err := getMetadata(file)
		if err != nil {
			log.Printf("Предупреждение: не удалось прочитать метаданные: %v", err)
		} else {
			displayMetadata(metadata)
		}

		// Удаляем метаданные
		if err := stripMetadata(ctx, file); err != nil {
			log.Printf("Ошибка обработки %s: %v", file, err)
			continue
		}

		// Проверяем что метаданные удалены
		if err := verifyMetadataRemoved(file); err != nil {
			log.Printf("Предупреждение: не удалось проверить удаление метаданных: %v", err)
		}

		log.Printf("[%d/%d] Готово: %s", i+1, len(videoFiles), filepath.Base(file))
	}

	log.Println("Обработка завершена")
}

// checkFFmpeg проверяет наличие ffmpeg в системе и при необходимости устанавливает его
func checkFFmpeg(ctx context.Context) error {
	// Проверяем наличие ffmpeg
	cmd := exec.Command("ffmpeg", "-version")
	if err := cmd.Run(); err == nil {
		return nil
	}

	log.Println("ffmpeg не найден. Попытка автоматической установки...")

	// Проверяем наличие Homebrew
	brewCmd := exec.Command("brew", "--version")
	if err := brewCmd.Run(); err != nil {
		return fmt.Errorf(
			"ffmpeg не найден и Homebrew недоступен.\n" +
				"Установите Homebrew: /bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\"\n" +
				"Затем установите ffmpeg: brew install ffmpeg",
		)
	}

	log.Println("Найден Homebrew. Устанавливаю ffmpeg...")
	log.Println("Это может занять несколько минут...")

	// Устанавливаем ffmpeg через brew с контекстом (таймаут 20 минут для установки)
	installCtx, installCancel := context.WithTimeout(ctx, 20*time.Minute)
	defer installCancel()

	installCmd := exec.CommandContext(installCtx, "brew", "install", "ffmpeg")
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr

	if err := installCmd.Run(); err != nil {
		if installCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("установка ffmpeg превысила таймаут (20 минут). Попробуйте установить вручную: brew install ffmpeg")
		}
		return fmt.Errorf("ошибка установки ffmpeg через brew: %w\nПопробуйте установить вручную: brew install ffmpeg", err)
	}

	log.Println("ffmpeg успешно установлен!")

	// Проверяем установку еще раз
	verifyCmd := exec.Command("ffmpeg", "-version")
	if err := verifyCmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg установлен, но недоступен в PATH. Перезапустите терминал или выполните: export PATH=\"/opt/homebrew/bin:$PATH\"")
	}

	return nil
}

// findVideoFiles находит все видеофайлы в указанной директории
func findVideoFiles(dir string) ([]string, error) {
	extMap := make(map[string]bool)
	for _, ext := range strings.Split(videoExtensions, ",") {
		extMap[strings.ToLower(ext)] = true
	}

	var videoFiles []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Пропускаем директории
		if info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if extMap[ext] {
			videoFiles = append(videoFiles, path)
		}

		return nil
	})

	return videoFiles, err
}

// metadataInfo структура для хранения метаданных
type metadataInfo struct {
	Format struct {
		Tags map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		Tags map[string]string `json:"tags"`
	} `json:"streams"`
}

// getMetadata получает метаданные из видеофайла используя ffprobe
func getMetadata(filePath string) (*metadataInfo, error) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		filePath,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения ffprobe: %w", err)
	}

	var info metadataInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("ошибка парсинга метаданных: %w", err)
	}

	return &info, nil
}

// displayMetadata выводит найденные метаданные
func displayMetadata(metadata *metadataInfo) {
	if metadata == nil {
		return
	}

	var foundMetadata []string

	// Проверяем метаданные контейнера
	if metadata.Format.Tags != nil {
		// Время создания
		if creationTime, ok := metadata.Format.Tags["creation_time"]; ok && creationTime != "" {
			foundMetadata = append(foundMetadata, fmt.Sprintf("creation_time: %s", creationTime))
		}

		// Энкодер
		if encoder, ok := metadata.Format.Tags["encoder"]; ok && encoder != "" {
			foundMetadata = append(foundMetadata, fmt.Sprintf("encoder: %s", encoder))
		}

		// Комментарии
		if comment, ok := metadata.Format.Tags["comment"]; ok && comment != "" {
			foundMetadata = append(foundMetadata, fmt.Sprintf("comment: %s", comment))
		}

		// Название
		if title, ok := metadata.Format.Tags["title"]; ok && title != "" {
			foundMetadata = append(foundMetadata, fmt.Sprintf("title: %s", title))
		}

		// Автор
		if artist, ok := metadata.Format.Tags["artist"]; ok && artist != "" {
			foundMetadata = append(foundMetadata, fmt.Sprintf("artist: %s", artist))
		}

		// Альбом
		if album, ok := metadata.Format.Tags["album"]; ok && album != "" {
			foundMetadata = append(foundMetadata, fmt.Sprintf("album: %s", album))
		}

		// Дата
		if date, ok := metadata.Format.Tags["date"]; ok && date != "" {
			foundMetadata = append(foundMetadata, fmt.Sprintf("date: %s", date))
		}

		// Описание
		if description, ok := metadata.Format.Tags["description"]; ok && description != "" {
			foundMetadata = append(foundMetadata, fmt.Sprintf("description: %s", description))
		}
	}

	// Проверяем метаданные потоков
	for i, stream := range metadata.Streams {
		if stream.Tags != nil {
			if creationTime, ok := stream.Tags["creation_time"]; ok && creationTime != "" {
				foundMetadata = append(foundMetadata, fmt.Sprintf("stream[%d].creation_time: %s", i, creationTime))
			}
			if encoder, ok := stream.Tags["encoder"]; ok && encoder != "" {
				foundMetadata = append(foundMetadata, fmt.Sprintf("stream[%d].encoder: %s", i, encoder))
			}
			if timecode, ok := stream.Tags["timecode"]; ok && timecode != "" {
				foundMetadata = append(foundMetadata, fmt.Sprintf("stream[%d].timecode: %s", i, timecode))
			}
		}
	}

	if len(foundMetadata) > 0 {
		log.Println("  Обнаружены метаданные:")
		for _, meta := range foundMetadata {
			log.Printf("    - %s", meta)
		}
		log.Println("  Удаляю метаданные...")
	} else {
		log.Println("  Метаданные не обнаружены")
	}
}

// verifyMetadataRemoved проверяет что метаданные удалены после обработки
func verifyMetadataRemoved(filePath string) error {
	metadata, err := getMetadata(filePath)
	if err != nil {
		return err
	}

	var remainingMetadata []string

	// Проверяем метаданные контейнера
	if metadata.Format.Tags != nil {
		// Игнорируем технические метаданные (major_brand, minor_version, compatible_brands)
		ignoredTags := map[string]bool{
			"major_brand":       true,
			"minor_version":     true,
			"compatible_brands": true,
		}

		for key, value := range metadata.Format.Tags {
			if !ignoredTags[key] && value != "" {
				// Игнорируем encoder от ffmpeg (Lavf*)
				if key == "encoder" && strings.HasPrefix(value, "Lavf") {
					continue
				}
				remainingMetadata = append(remainingMetadata, fmt.Sprintf("%s: %s", key, value))
			}
		}
	}

	// Проверяем метаданные потоков (только критичные)
	for i, stream := range metadata.Streams {
		if stream.Tags != nil {
			if creationTime, ok := stream.Tags["creation_time"]; ok && creationTime != "" {
				remainingMetadata = append(remainingMetadata, fmt.Sprintf("stream[%d].creation_time: %s", i, creationTime))
			}
			if encoder, ok := stream.Tags["encoder"]; ok && encoder != "" && !strings.HasPrefix(encoder, "Lav") {
				remainingMetadata = append(remainingMetadata, fmt.Sprintf("stream[%d].encoder: %s", i, encoder))
			}
			if timecode, ok := stream.Tags["timecode"]; ok && timecode != "" {
				remainingMetadata = append(remainingMetadata, fmt.Sprintf("stream[%d].timecode: %s", i, timecode))
			}
		}
	}

	if len(remainingMetadata) > 0 {
		log.Println("  ⚠️  Предупреждение: остались метаданные:")
		for _, meta := range remainingMetadata {
			log.Printf("    - %s", meta)
		}
	} else {
		log.Println("  ✓ Метаданные успешно удалены")
	}

	return nil
}

// stripMetadata удаляет метаданные из видеофайла используя ffmpeg
func stripMetadata(ctx context.Context, inputFile string) error {
	// Создаем временный файл с тем же расширением что и исходный файл
	ext := filepath.Ext(inputFile)
	baseName := strings.TrimSuffix(inputFile, ext)
	tmpFile := baseName + ".tmp" + ext

	defer func() {
		// Удаляем временный файл в случае ошибки
		if _, err := os.Stat(tmpFile); err == nil {
			if removeErr := os.Remove(tmpFile); removeErr != nil {
				log.Printf("Предупреждение: не удалось удалить временный файл %s: %v", tmpFile, removeErr)
			}
		}
	}()

	// Команда ffmpeg для удаления метаданных
	// -map_metadata -1: удаляет все метаданные
	// -c copy: копирует потоки без перекодирования (быстро)
	// -y: перезаписывает выходной файл без запроса
	// -loglevel error: показывает только ошибки
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-loglevel", "error",
		"-i", inputFile,
		"-map_metadata", "-1",
		"-c", "copy",
		"-y",
		tmpFile,
	)

	// Перенаправляем вывод ffmpeg в stderr для логирования ошибок
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ошибка выполнения ffmpeg: %w", err)
	}

	// Проверяем, что временный файл создан
	if _, err := os.Stat(tmpFile); os.IsNotExist(err) {
		return fmt.Errorf("временный файл не был создан")
	}

	// Заменяем оригинальный файл
	if err := os.Rename(tmpFile, inputFile); err != nil {
		return fmt.Errorf("ошибка замены файла: %w", err)
	}

	return nil
}
