package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

/*
1. Ask for email
2. Ask for password
3. Login user with credentials and save access token in-script
4. Initiate a hash of key {index} values hash of trainers details(name, email)
5. Loop through the hash.
6. For every index:
	a. communicate with the api -> create trainer, providing
	b. If communication with api fails, save into a seperate array of failed logins
7. Maually retry failed task
8. If everything is done, show success message
*/

type AdminResponse struct {
	Data AdminResponseTokens `json:"data"`
}

type AdminResponseTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type ErrorResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type ValidationErrorResponse struct {
	Message string             `json:"message"`
	Errors  []ValidationErrors `json:"errors"`
}
type ValidationErrors struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type TrainersMap map[int]Trainer

var TrainersToBeCreated = make(TrainersMap)

// type Trainer map[string]any

type Trainer struct {
	Name              string
	Email             string
	PhoneNumber       *string
	Specializations   []*string
	YearsOfExperience *string
	Bio               *string
	Gender            *string
}

var RandomSpecializations = []string{"yoga", "speed", "cardio", "endurance", "strength"}
var RandomGender = []string{"male", "female"}

func strPtr(s string) *string {
	return &s
}

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Printf("Error loading .env file: %v", err)
		os.Exit(1)
	}
	var fileLocation string
	BASE_URL := os.Getenv("API_BASE_URL")
	if BASE_URL == "" {
		BASE_URL = "http://localhost:8080/api/v1"
	}

	// Receive file location
	fmt.Println("Please enter text file location containing email")
	if _, err := fmt.Scan(&fileLocation); err != nil {
		fmt.Printf("failed to read text file location: %v", err)
		os.Exit(1)
	}
	fmt.Println("")
	fmt.Println("Press Enter to continue...")
	_, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		fmt.Printf("failed to read input: %v", err)
	}
	fmt.Println("📍reading file from location...")

	// Read text file
	file, err := os.Open(fileLocation)
	if err != nil {
		fmt.Printf("failed to read file from locaion: %v", err)
		os.Exit(1)
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("failed to close file: %v", err)
		}
	}()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		email := strings.TrimSpace(scanner.Text())
		if email == "" {
			continue
		}

		name := strings.SplitN(email, "@", 2)[0]
		TrainersToBeCreated[len(TrainersToBeCreated)] = Trainer{
			Email: email,
			Name:  name,
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Printf("failed to read line from file: %v", err)
		return
	}
	fmt.Println("✅ read emails from text file complete ")
	fmt.Println("")
	if err := createTrainers(BASE_URL); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func createTrainers(BASE_URL string) error {
	var email string
	var password string
	var retryFailedTrainers string

	fmt.Print("Please provide an admin email: ")
	if _, err := fmt.Scan(&email); err != nil {
		return fmt.Errorf("failed to read admin email: %v", err)
	}
	fmt.Print("Please provide an admin password: ")
	if _, err := fmt.Scan(&password); err != nil {
		return fmt.Errorf("failed to read admin password: %v", err)
	}

	client := &http.Client{}
	accessToken, err := generateAccessToken(BASE_URL, client, email, password)
	if err != nil {
		return fmt.Errorf("failed to generate access token: %v", err)
	}
	fmt.Println("✅ Access token generated successfully.")

	// create trainer account
	failedTrainers, err := createTrainerAcct(BASE_URL, client, *accessToken, TrainersToBeCreated)
	if err != nil {
		return fmt.Errorf("failed to create trainers accounts: %v", err)
	}

	if len(failedTrainers) > 0 {
		// retry for failed trainers
		fmt.Println("")
		fmt.Println("⚠️  Failed to create accounts for the following trainers: ")
		for index, failed := range failedTrainers {
			fmt.Printf("%d. %s\n", index+1, failed.Email)
		}
		fmt.Println("Do you wish to retry failed trainers? (Enter Y if 'yes', else enter N): ")
		if _, err := fmt.Scan(&retryFailedTrainers); err != nil {
			return fmt.Errorf("failed to read value: %v", err)
		}
		input := strings.ToLower(strings.TrimSpace(retryFailedTrainers))
		if input == "yes" || input == "y" {
			if _, err := createTrainerAcct(BASE_URL, client, *accessToken, failedTrainers); err != nil {
				return fmt.Errorf("%v", err)
			}
		} else {
			return nil
		}
	}
	fmt.Println("🫱🏿‍🫲🏽 ✅ Your trainers have been created, and can check their mails.")
	return nil
}

func convertStructToReader(payload map[string]interface{}) (io.Reader, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		return nil, err
	}
	return &buf, nil
}

func createTrainerAcct(base_url string, client *http.Client, access_token string, trainersMap TrainersMap) (TrainersMap, error) {
	endpoint := base_url + "/trainers"
	var failedTrainers = make(TrainersMap)
	// var body map[string]interface{}
	for index, trainer := range trainersMap {
		randomGenderNo := rand.IntN(len(RandomGender))
		specRandomNumber := rand.IntN(len(RandomSpecializations))
		t := &Trainer{
			Email: trainer.Email,
			Name:  trainer.Name,
		}
		if trainer.PhoneNumber != nil {
			t.PhoneNumber = trainer.PhoneNumber
		} else {
			t.PhoneNumber = strPtr("+234801234567" + fmt.Sprint(index))
		}
		if trainer.Gender != nil {
			t.Gender = trainer.Gender
		} else {
			t.Gender = strPtr(RandomGender[randomGenderNo])
		}
		if len(trainer.Specializations) > 0 {
			t.Specializations = trainer.Specializations
		} else {
			t.Specializations = []*string{strPtr(RandomSpecializations[specRandomNumber])}
		}
		if trainer.YearsOfExperience != nil {
			t.YearsOfExperience = trainer.YearsOfExperience
		} else {
			t.YearsOfExperience = strPtr(fmt.Sprint(rand.IntN(10)))
		}
		if trainer.Bio != nil {
			t.Bio = trainer.Bio
		} else {
			t.Bio = strPtr("Certified personal trainer with a passion for helping clients achieve their fitness goals.")
		}

		fmt.Println("")
		fmt.Printf("📤 Creating trainer for person - %d\n", index)
		var requestBody bytes.Buffer
		multipartWriter := multipart.NewWriter(&requestBody)
		if err := writeDataIntoMultiField(multipartWriter, "email", t.Email, false, index, trainer, failedTrainers); err != nil {
			fmt.Printf("%v\n", err)
			continue
		}
		if err := writeDataIntoMultiField(multipartWriter, "name", t.Name, false, index, trainer, failedTrainers); err != nil {
			fmt.Printf("%v\n", err)
			continue
		}
		if err := writeDataIntoMultiField(multipartWriter, "phone_number", *t.PhoneNumber, false, index, trainer, failedTrainers); err != nil {
			fmt.Printf("%v\n", err)
			continue
		}
		if err := writeDataIntoMultiField(multipartWriter, "gender", *t.Gender, false, index, trainer, failedTrainers); err != nil {
			fmt.Printf("%v\n", err)
			continue
		}
		failedSpecialization := false
		for _, s := range t.Specializations {
			if err := writeDataIntoMultiField(multipartWriter, "specializations", *s, false, index, trainer, failedTrainers); err != nil {
				fmt.Printf("%v\n", err)
				failedSpecialization = true
				break
			}
		}
		if failedSpecialization {
			continue
		}
		if err := writeDataIntoMultiField(multipartWriter, "years_of_experience", *t.YearsOfExperience, false, index, trainer, failedTrainers); err != nil {
			fmt.Printf("%v\n", err)
			continue
		}
		if err := writeDataIntoMultiField(multipartWriter, "bio", *t.Bio, true, index, trainer, failedTrainers); err != nil {
			fmt.Printf("%v\n", err)
			continue
		}
		if err := multipartWriter.Close(); err != nil {
			return failedTrainers, fmt.Errorf("failed to close multipart writer: %v", err)
		}
		// send request
		req, err := http.NewRequest(http.MethodPost, endpoint, &requestBody)
		if err != nil {
			fmt.Printf("❌ failed to create new request: %v\n", err)
			appendIntoFailedTrainer(index, trainer, failedTrainers)
			continue
		}
		req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+access_token)

		res, err := client.Do(req)
		if err != nil {
			appendIntoFailedTrainer(index, trainer, failedTrainers)
			fmt.Printf("❌ failed to make request to %v: %v\n", req.URL.String(), err)
			continue
		}

		if res.StatusCode != http.StatusCreated {
			if res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusConflict {
				var response ValidationErrorResponse
				respBody, err := io.ReadAll(res.Body)
				if closeErr := res.Body.Close(); closeErr != nil {
					return failedTrainers, fmt.Errorf("failed to close response body: %v", err)
				}
				if err != nil {
					fmt.Printf("❌ failed to read response body: %v\n", err)
				} else {
					if err := json.Unmarshal(respBody, &response); err != nil {
						fmt.Printf("❌ failed to unmarshal response body: %v\n", err)
					} else {
						fmt.Printf("❌ failed to create trainer %v: %v\n", trainer.Email, response.Message)
						for _, validationErr := range response.Errors {
							fmt.Printf("   - field '%v': %v\n", validationErr.Field, validationErr.Message)
						}
					}
				}
			}
			appendIntoFailedTrainer(index, trainer, failedTrainers)
			fmt.Printf("❌ failed to create trainer %v: receive status code: %v\n", trainer.Email, res.StatusCode)

			continue
		} else {
			fmt.Printf("✅ Created trainer with email: %v\n", trainer.Email)
			if err := res.Body.Close(); err != nil {
				return failedTrainers, fmt.Errorf("failed to close response body: %v", err)
			}
		}

		time.Sleep(200 * time.Millisecond) // slight delay to avoid overwhelming the server
	}
	return failedTrainers, nil
}

func appendIntoFailedTrainer(index int, trainer Trainer, failedTrainer TrainersMap) {
	failedTrainer[index] = trainer
}

func generateAccessToken(base_url string, client *http.Client, email, password string) (*string, error) {
	var accessToken string
	// login user
	loginEndpoint := fmt.Sprintf("%s/auth/admin/log-in", base_url)
	loginBody := map[string]interface{}{
		"email":    email,
		"password": password,
	}
	loginPayload, err := convertStructToReader(loginBody)
	if err != nil {
		return nil, fmt.Errorf("failed to parse login payload: %v", err)
	}
	res, err := client.Post(loginEndpoint, "application/json", loginPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to send login request to admin endpoint: %v", err)
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			slog.Warn("failed to close response body", "error", err)
		}
	}()
	if res.StatusCode != http.StatusOK {
		var errorRes ErrorResponse
		respBody, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read admin login error response: %v", err)
		}
		if err := json.Unmarshal(respBody, &errorRes); err != nil {
			return nil, fmt.Errorf("failed to unmarshal admin login error response: %v", err)
		}
		return nil, fmt.Errorf("%v", errorRes.Message)
	}
	var successResp AdminResponse
	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read admin login success response: %v", err)
	}
	if err := json.Unmarshal(respBody, &successResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal admin login success response: %v", err)
	}
	// fmt.Printf("%v\n", successResp)
	accessToken = successResp.Data.AccessToken
	return &accessToken, nil
}

func writeDataIntoMultiField(multipartWriter *multipart.Writer, field, value string, isFieldOptional bool, index int, trainer Trainer, failedtrainer TrainersMap) error {
	if isFieldOptional {
		if strings.TrimSpace(value) != "" {
			if err := multipartWriter.WriteField(field, value); err != nil {
				appendIntoFailedTrainer(index, trainer, failedtrainer)
				return fmt.Errorf("❌ failed to write %v field: %v", field, err)
			}
		}
	} else {
		if strings.TrimSpace(value) != "" {
			if err := multipartWriter.WriteField(field, value); err != nil {
				appendIntoFailedTrainer(index, trainer, failedtrainer)
				return fmt.Errorf("❌ failed to write %v field: %v", field, err)
			}
		} else {
			appendIntoFailedTrainer(index, trainer, failedtrainer)
			return fmt.Errorf("❌ field '%v' is required", field)
		}
	}
	return nil
}
