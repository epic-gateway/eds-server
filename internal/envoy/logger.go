// Copyright 2020 Envoyproxy Authors
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

package envoy

import (
	"fmt"

	"github.com/go-logr/logr"
)

// Logger is an example of a logger that implements pkg/log/Logger.
// It logs to stdout.  If debug == false then Debugf() and Infof()
// won't output anything.
type Logger struct {
	Logger logr.Logger
	Debug  bool
}

// Debugf logs to stdout only if debug == true.
func (logger Logger) Debugf(format string, args ...interface{}) {
	logger.Logger.V(1).Info(fmt.Sprintf(format, args...))
}

// Infof logs to stdout only if debug == true.
func (logger Logger) Infof(format string, args ...interface{}) {
	logger.Logger.Info(fmt.Sprintf(format, args...))
}

// Warnf logs to stdout always.
func (logger Logger) Warnf(format string, args ...interface{}) {
	logger.Logger.Info(fmt.Sprintf(format, args...))
}

// Errorf logs to stdout always.
func (logger Logger) Errorf(format string, args ...interface{}) {
	logger.Logger.Error(fmt.Errorf("xds cache error"), fmt.Sprintf(format, args...))
}
