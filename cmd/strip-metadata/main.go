package main

import (
	"context"
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

	// Проверяем наличие ffmpeg
	if err := checkFFmpeg(); err != nil {
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
		log.Printf("[%d/%d] Обработка: %s", i+1, len(videoFiles), filepath.Base(file))

		if err := stripMetadata(ctx, file); err != nil {
			log.Printf("Ошибка обработки %s: %v", file, err)
			continue
		}

		log.Printf("[%d/%d] Готово: %s", i+1, len(videoFiles), filepath.Base(file))
	}

	log.Println("Обработка завершена")
}

// checkFFmpeg проверяет наличие ffmpeg в системе
func checkFFmpeg() error {
	cmd := exec.Command("ffmpeg", "-version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg не найден в PATH. Установите ffmpeg: brew install ffmpeg")
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

