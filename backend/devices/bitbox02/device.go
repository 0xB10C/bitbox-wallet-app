// Copyright 2018 Shift Devices AG
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

// Package bitbox02 implements the Device and Keystore interfaces to integrate the bitbox02 into the
// app.
package bitbox02

import (
	"fmt"
	"sync"

	event "github.com/digitalbitbox/bitbox-wallet-app/backend/devices/device/event"
	keystoreInterface "github.com/digitalbitbox/bitbox-wallet-app/backend/keystore"
	"github.com/digitalbitbox/bitbox-wallet-app/backend/signing"
	"github.com/digitalbitbox/bitbox-wallet-app/util/logging"
	bitbox02common "github.com/digitalbitbox/bitbox02-api-go/api/common"
	"github.com/digitalbitbox/bitbox02-api-go/api/firmware"
	"github.com/digitalbitbox/bitbox02-api-go/util/semver"
	"github.com/sirupsen/logrus"
)

// ProductName is the name of the BitBox02 product.
// If you change this, be sure to check the frontend and other places which assume this is a
// constant.
const ProductName = "bitbox02"

// Device implements device.Device.
type Device struct {
	firmware.Device
	deviceID string
	mu       sync.RWMutex
	onEvent  func(event.Event, interface{})
	log      *logrus.Entry
}

// NewDevice creates a new instance of Device.
func NewDevice(
	deviceID string,
	version *semver.SemVer,
	edition bitbox02common.Edition,
	config firmware.ConfigInterface,
	communication firmware.Communication,
) *Device {
	log := logging.Get().
		WithGroup("device").
		WithField("deviceID", deviceID).
		WithField("productName", ProductName)

	log.Info("Plugged in device")
	device := &Device{
		Device: *firmware.NewDevice(
			version,
			edition,
			config,
			communication, logger{log},
		),
		deviceID: deviceID,
		log:      log,
	}
	device.Device.SetOnEvent(func(ev firmware.Event, meta interface{}) {
		device.fireEvent(event.Event(ev))
		switch ev {
		case firmware.EventStatusChanged:
			switch device.Device.Status() {
			case firmware.StatusInitialized:
				device.fireEvent(event.EventKeystoreAvailable)
			}
		}
	})
	return device
}

// Init implements device.Device.
func (device *Device) Init(testing bool) error {
	return device.Device.Init()
}

// ProductName implements device.Device.
func (device *Device) ProductName() string {
	return ProductName
}

// Identifier implements device.Device.
func (device *Device) Identifier() string {
	return device.deviceID
}

// KeystoreForConfiguration implements device.Device.
func (device *Device) KeystoreForConfiguration(configuration *signing.Configuration, cosignerIndex int) keystoreInterface.Keystore {
	if device.Status() != firmware.StatusInitialized {
		return nil
	}
	return &keystore{
		device:        device,
		configuration: configuration,
		cosignerIndex: cosignerIndex,
		log:           device.log,
	}
}

func (device *Device) fireEvent(event event.Event) {
	device.mu.RLock()
	f := device.onEvent
	device.mu.RUnlock()
	if f != nil {
		device.log.Info(fmt.Sprintf("fire event: %s", event))
		f(event, nil)
	}
}

// SetOnEvent implements device.Device.
func (device *Device) SetOnEvent(onEvent func(event.Event, interface{})) {
	device.mu.Lock()
	defer device.mu.Unlock()
	device.onEvent = onEvent
}

// Reset factory resets the device.
func (device *Device) Reset() error {
	if err := device.Device.Reset(); err != nil {
		return err
	}
	device.fireEvent(event.EventKeystoreGone)
	return nil
}
