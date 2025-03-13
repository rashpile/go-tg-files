package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"gopkg.in/yaml.v2"
)

// Configuration constants
const (
	configPath = "./config.yml" // Path to configuration file
)

// CategoryConfig represents a category configuration
type CategoryConfig struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

// Config represents the application configuration
type Config struct {
	Categories []CategoryConfig `yaml:"categories"`
}

// Global variables
var (
	config       Config
	categoryMap  = make(map[string]string) // Map of category name to path
	userDefaults = make(map[int64]string)  // Map of user ID to default category
)

func main() {
	// Try to get bot token from .env file first, then fall back to environment variable
	botToken := readBotTokenFromEnvFile()
	if botToken == "" {
		botToken = os.Getenv("TELEGRAM_BOT_TOKEN")
		if botToken == "" {
			log.Fatal("TELEGRAM_BOT_TOKEN not found in .env file or environment variables")
		}
	}

	// Load configuration
	if err := loadConfig(); err != nil {
		log.Printf("Error loading config: %v. Using default categories.", err)
		setupDefaultCategories()
	}

	// Create bot instance
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Fatal("Error creating bot:", err)
	}

	// Uncomment for debugging
	// bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	// Create storage directories
	createStorageDirectories()

	// Configure update settings
	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60

	// Start receiving updates
	updates := bot.GetUpdatesChan(updateConfig)

	// Handle updates
	for update := range updates {
		if update.Message == nil {
			continue
		}

		// Handle commands
		if update.Message.IsCommand() {
			handleCommand(bot, update.Message)
			continue
		}

		// Handle file messages
		if hasAttachment(update.Message) {
			handleFileMessage(bot, update.Message)
		} else if update.Message.Text != "" {
			// Handle text messages that are not commands
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Please send a file with an optional category in caption. Example: /image vacation.jpg")
			bot.Send(msg)
		}
	}
}

// Load configuration from YAML file
func loadConfig() error {
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return err
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return err
	}

	// Build category map
	for _, cat := range config.Categories {
		categoryMap[cat.Name] = cat.Path
		log.Printf("Loaded category: %s -> %s", cat.Name, cat.Path)
	}

	return nil
}

// Setup default categories if config file is not available
func setupDefaultCategories() {
	defaultCategories := []CategoryConfig{
		{Name: "document", Path: "./files/documents"},
		{Name: "image", Path: "./files/images"},
		{Name: "video", Path: "./files/videos"},
		{Name: "audio", Path: "./files/audio"},
		{Name: "other", Path: "./files/misc"},
	}

	config.Categories = defaultCategories

	// Build category map
	for _, cat := range defaultCategories {
		categoryMap[cat.Name] = cat.Path
		log.Printf("Using default category: %s -> %s", cat.Name, cat.Path)
	}
}

// Check if message has any file attachment
func hasAttachment(message *tgbotapi.Message) bool {
	return message.Document != nil || len(message.Photo) > 0 || message.Video != nil ||
		message.Audio != nil || message.Voice != nil || message.VideoNote != nil
}

// Handle bot commands
func handleCommand(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	cmd := message.Command()
	args := message.CommandArguments()

	switch cmd {
	case "start":
		sendStartMessage(bot, message)
	case "help":
		sendHelpMessage(bot, message)
	case "categories":
		sendCategoriesMessage(bot, message)
	case "setdefault":
		handleSetDefaultCommand(bot, message, args)
	case "unsetdefault":
		handleUnsetDefaultCommand(bot, message)
	default:
		// Check if command is a category name
		if path, exists := categoryMap[cmd]; exists {
			msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Selected category: %s (path: %s)\nNow send me a file to save it in this category.", cmd, path))
			bot.Send(msg)
			return
		}

		msg := tgbotapi.NewMessage(message.Chat.ID, "Unknown command. Type /help for available commands.")
		bot.Send(msg)
	}
}

// Send welcome message
func sendStartMessage(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	welcomeText := fmt.Sprintf("Welcome, %s! I'm a file saving bot. Send me files and I'll save them for you.\n\nUse /help to see available commands.", message.From.FirstName)
	msg := tgbotapi.NewMessage(message.Chat.ID, welcomeText)
	bot.Send(msg)
}

// Send help message
func sendHelpMessage(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	helpText := `
Available commands:
/start - Start the bot
/help - Show this help message
/categories - List available file categories
/setdefault [category] - Set default category for saving files
/unsetdefault - Remove default category setting

To save a file with a specific category, send the file with a caption in the format: 
/category filename

Example: /image vacation.jpg

If no category is specified, I'll use your default category (if set) or determine it automatically based on file type.
`
	msg := tgbotapi.NewMessage(message.Chat.ID, helpText)
	bot.Send(msg)
}

// Send categories message
func sendCategoriesMessage(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	categoriesText := "Available categories for file organization:\n"
	for catName, catPath := range categoryMap {
		categoriesText += fmt.Sprintf("/%s - Save file to %s folder\n", catName, catPath)
	}
	msg := tgbotapi.NewMessage(message.Chat.ID, categoriesText)
	bot.Send(msg)
}

// Handle set default category command
func handleSetDefaultCommand(bot *tgbotapi.BotAPI, message *tgbotapi.Message, args string) {
	if args == "" {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Please specify a category. Usage: /setdefault [category]")
		bot.Send(msg)
		return
	}

	// Check if category exists
	if _, exists := categoryMap[args]; !exists {
		availableCategories := make([]string, 0, len(categoryMap))
		for cat := range categoryMap {
			availableCategories = append(availableCategories, cat)
		}
		msg := tgbotapi.NewMessage(
			message.Chat.ID,
			fmt.Sprintf("Category '%s' does not exist. Available categories: %s",
				args, strings.Join(availableCategories, ", ")),
		)
		bot.Send(msg)
		return
	}

	// Set default category for user
	userDefaults[message.From.ID] = args
	msg := tgbotapi.NewMessage(
		message.Chat.ID,
		fmt.Sprintf("Default category set to '%s'. All your files will be saved to this category unless specified otherwise.", args),
	)
	bot.Send(msg)
}

// Handle unset default category command
func handleUnsetDefaultCommand(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	// Check if user has a default category
	if _, exists := userDefaults[message.From.ID]; !exists {
		msg := tgbotapi.NewMessage(message.Chat.ID, "You don't have a default category set.")
		bot.Send(msg)
		return
	}

	// Remove default category for user
	delete(userDefaults, message.From.ID)
	msg := tgbotapi.NewMessage(
		message.Chat.ID,
		"Default category removed. Files will be categorized automatically based on type.",
	)
	bot.Send(msg)
}

// Handle file messages
func handleFileMessage(bot *tgbotapi.BotAPI, message *tgbotapi.Message) {
	// Extract category from caption if present
	category := ""
	customFilename := ""

	if message.Caption != "" {
		parts := strings.Split(message.Caption, " ")
		if len(parts) > 0 && strings.HasPrefix(parts[0], "/") {
			requestedCategory := strings.TrimPrefix(parts[0], "/")
			if _, ok := categoryMap[requestedCategory]; ok {
				category = requestedCategory
			}

			// Check if custom filename is provided after category
			if len(parts) > 1 {
				customFilename = strings.Join(parts[1:], " ")
			}
		}
	}

	// If no category specified in caption, check for user default
	if category == "" {
		if defaultCat, hasDefault := userDefaults[message.From.ID]; hasDefault {
			category = defaultCat
		} else {
			// If no default, determine based on file type
			category = determineCategory(message)
		}
	}

	// Get file info
	fileID, originalFilename := getFileInfo(message)
	if fileID == "" {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Could not process this file.")
		bot.Send(msg)
		return
	}

	// Use custom filename if provided, otherwise use original
	filename := originalFilename
	if customFilename != "" {
		// Keep the original extension if present
		originalExt := filepath.Ext(originalFilename)
		customExt := filepath.Ext(customFilename)

		if customExt == "" && originalExt != "" {
			customFilename += originalExt
		}
		filename = customFilename
	}

	// Get storage path for category
	storagePath, ok := categoryMap[category]
	if !ok {
		// Fallback to misc if category not found (should not happen)
		storagePath = categoryMap["other"]
		if storagePath == "" {
			storagePath = "./files/misc"
		}
	}

	// Status message to user
	statusMsg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Saving file '%s' to category '%s' (path: %s)...", filename, category, storagePath))
	statusMessage, _ := bot.Send(statusMsg)

	// Download and save the file
	savedPath, err := downloadAndSaveFile(bot, fileID, storagePath, filename)
	if err != nil {
		errorMsg := tgbotapi.NewEditMessageText(message.Chat.ID, statusMessage.MessageID, fmt.Sprintf("Error saving file: %s", err.Error()))
		bot.Send(errorMsg)
		return
	}

	// Success message
	successMsg := tgbotapi.NewEditMessageText(
		message.Chat.ID,
		statusMessage.MessageID,
		fmt.Sprintf("File saved successfully!\nCategory: %s\nLocation: %s", category, savedPath),
	)
	bot.Send(successMsg)
}

// Get file info (ID and filename) from message
func getFileInfo(message *tgbotapi.Message) (string, string) {
	if message.Document != nil {
		return message.Document.FileID, message.Document.FileName
	} else if len(message.Photo) > 0 {
		// Get the largest photo (last in the array)
		photo := message.Photo[len(message.Photo)-1]
		// Photos don't have filenames, generate one based on date
		return photo.FileID, fmt.Sprintf("photo_%d.jpg", time.Now().Unix())
	} else if message.Video != nil {
		filename := message.Video.FileName
		if filename == "" {
			filename = fmt.Sprintf("video_%d.mp4", time.Now().Unix())
		}
		return message.Video.FileID, filename
	} else if message.Audio != nil {
		filename := message.Audio.FileName
		if filename == "" {
			filename = fmt.Sprintf("audio_%d.mp3", time.Now().Unix())
		}
		return message.Audio.FileID, filename
	} else if message.Voice != nil {
		return message.Voice.FileID, fmt.Sprintf("voice_%d.ogg", time.Now().Unix())
	} else if message.VideoNote != nil {
		return message.VideoNote.FileID, fmt.Sprintf("video_note_%d.mp4", time.Now().Unix())
	}
	return "", ""
}

// Determine category based on file type
func determineCategory(message *tgbotapi.Message) string {
	if message.Document != nil {
		return "document"
	} else if len(message.Photo) > 0 {
		return "image"
	} else if message.Video != nil || message.VideoNote != nil {
		return "video"
	} else if message.Audio != nil || message.Voice != nil {
		return "audio"
	}
	return "other"
}

// Download and save file
func downloadAndSaveFile(bot *tgbotapi.BotAPI, fileID, storagePath, filename string) (string, error) {
	// Get file URL
	fileURL, err := bot.GetFileDirectURL(fileID)
	if err != nil {
		return "", fmt.Errorf("error getting file URL: %w", err)
	}

	// Sanitize filename
	safeFilename := sanitizeFilename(filename)

	// Create directory
	if err := os.MkdirAll(storagePath, 0755); err != nil {
		return "", fmt.Errorf("error creating directory: %w", err)
	}

	// Create unique filename if file already exists
	finalPath := filepath.Join(storagePath, safeFilename)
	finalPath = ensureUniqueFilename(finalPath)

	// Download file
	resp, err := http.Get(fileURL)
	if err != nil {
		return "", fmt.Errorf("error downloading file: %w", err)
	}
	defer resp.Body.Close()

	// Create file
	outFile, err := os.Create(finalPath)
	if err != nil {
		return "", fmt.Errorf("error creating file: %w", err)
	}
	defer outFile.Close()

	// Copy data
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return "", fmt.Errorf("error writing file: %w", err)
	}

	return finalPath, nil
}

// Create storage directories
func createStorageDirectories() {
	for _, path := range categoryMap {
		if err := os.MkdirAll(path, 0755); err != nil {
			log.Printf("Error creating directory %s: %v", path, err)
		}
	}
}

// Sanitize filename to make it safe for filesystem
func sanitizeFilename(filename string) string {
	// List of invalid characters in filenames
	invalidChars := []string{"\\", "/", ":", "*", "?", "\"", "<", ">", "|"}

	result := filename
	for _, char := range invalidChars {
		result = strings.ReplaceAll(result, char, "_")
	}

	// Limit filename length
	if len(result) > 240 {
		ext := filepath.Ext(result)
		result = result[:240-len(ext)] + ext
	}

	return result
}

// Ensure filename is unique by adding number if needed
func ensureUniqueFilename(filePath string) string {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return filePath // File doesn't exist, use as is
	}

	// File exists, add number
	dir := filepath.Dir(filePath)
	ext := filepath.Ext(filePath)
	name := filepath.Base(filePath[:len(filePath)-len(ext)])

	for i := 1; ; i++ {
		newPath := filepath.Join(dir, fmt.Sprintf("%s_%d%s", name, i, ext))
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			return newPath
		}
	}
}

// Read bot token from .env file
func readBotTokenFromEnvFile() string {
	// Check if .env file exists
	envFile := ".env"
	if _, err := os.Stat(envFile); os.IsNotExist(err) {
		return "" // File doesn't exist
	}

	// Read file content
	data, err := ioutil.ReadFile(envFile)
	if err != nil {
		log.Printf("Error reading .env file: %v", err)
		return ""
	}

	// Parse file content line by line
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		// Skip empty lines and comments
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Look for TELEGRAM_BOT_TOKEN=value
		if strings.HasPrefix(line, "TELEGRAM_BOT_TOKEN=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				token := strings.TrimSpace(parts[1])
				// Remove quotes if present
				token = strings.Trim(token, "\"'")
				return token
			}
		}
	}

	return "" // Token not found in .env file
}
