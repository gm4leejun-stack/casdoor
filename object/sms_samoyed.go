// Copyright 2026 The Casdoor Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package object

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/casdoor/casdoor/conf"
)

const samoyedSmsProvider = "Samoyed SMS"

var samoyedMobilePattern = regexp.MustCompile(`^\+861[3-9]\d{9}$`)

type samoyedSmsClient struct {
	url             string
	businessCode    string
	messageEnumCode string
	mobileAesKey    []byte
	httpClient      *http.Client
}

type samoyedSmsRequest struct {
	MessageEnumCode string            `json:"messageEnumCode"`
	BusinessCode    string            `json:"businessCode"`
	Mobile          string            `json:"mobile"`
	Timestamp       int64             `json:"timestamp"`
	Param           map[string]string `json:"param"`
}

func newSamoyedSmsClient(httpClient *http.Client) (*samoyedSmsClient, error) {
	settings := map[string]string{
		"smsUrl":             conf.GetConfigString("smsUrl"),
		"smsBusinessCode":    conf.GetConfigString("smsBusinessCode"),
		"smsMessageEnumCode": conf.GetConfigString("smsMessageEnumCode"),
		"smsMobileAesKey":    conf.GetConfigString("smsMobileAesKey"),
	}
	for name, value := range settings {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("Samoyed SMS: %s is required", name)
		}
	}

	key := []byte(settings["smsMobileAesKey"])
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, fmt.Errorf("Samoyed SMS: smsMobileAesKey must be 16, 24, or 32 UTF-8 bytes")
	}

	return &samoyedSmsClient{
		url:             settings["smsUrl"],
		businessCode:    settings["smsBusinessCode"],
		messageEnumCode: settings["smsMessageEnumCode"],
		mobileAesKey:    key,
		httpClient:      httpClient,
	}, nil
}

func (client *samoyedSmsClient) SendMessage(param map[string]string, targetPhoneNumber ...string) error {
	if len(targetPhoneNumber) != 1 || !samoyedMobilePattern.MatchString(targetPhoneNumber[0]) {
		return fmt.Errorf("Samoyed SMS: exactly one mainland China phone number in +86 format is required")
	}

	mobile, err := encryptSamoyedMobile(strings.TrimPrefix(targetPhoneNumber[0], "+86"), client.mobileAesKey)
	if err != nil {
		return fmt.Errorf("Samoyed SMS: failed to encrypt phone number: %w", err)
	}

	body, err := json.Marshal(samoyedSmsRequest{
		MessageEnumCode: client.messageEnumCode,
		BusinessCode:    client.businessCode,
		Mobile:          mobile,
		Timestamp:       time.Now().UnixMilli(),
		Param:           map[string]string{"code": param["code"]},
	})
	if err != nil {
		return fmt.Errorf("Samoyed SMS: failed to build request: %w", err)
	}

	request, err := http.NewRequest(http.MethodPost, client.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("Samoyed SMS: invalid smsUrl: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("Samoyed SMS: request failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, response.Body)
		return fmt.Errorf("Samoyed SMS: upstream returned HTTP %d", response.StatusCode)
	}

	var result struct {
		Code json.RawMessage `json:"code"`
	}
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		return fmt.Errorf("Samoyed SMS: invalid upstream response")
	}

	var code string
	if err = json.Unmarshal(result.Code, &code); err != nil {
		var number json.Number
		if err = json.Unmarshal(result.Code, &number); err != nil {
			return fmt.Errorf("Samoyed SMS: invalid upstream response code")
		}
		code = number.String()
	}
	if code != strconv.Itoa(http.StatusOK) {
		return fmt.Errorf("Samoyed SMS: upstream returned code %s", code)
	}

	return nil
}

func encryptSamoyedMobile(mobile string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	padding := block.BlockSize() - len(mobile)%block.BlockSize()
	plainText := append([]byte(mobile), bytes.Repeat([]byte{byte(padding)}, padding)...)
	cipherText := make([]byte, len(plainText))
	for offset := 0; offset < len(plainText); offset += block.BlockSize() {
		block.Encrypt(cipherText[offset:offset+block.BlockSize()], plainText[offset:offset+block.BlockSize()])
	}

	return base64.StdEncoding.EncodeToString(cipherText), nil
}
