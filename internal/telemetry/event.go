// Copyright 2024
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package telemetry

import (
	"github.com/segmentio/analytics-go"

	"github.com/Mirantis/hmc/internal/build"
)

const (
	deploymentCreateEvent = "deployment-create"
)

func TrackDeploymentCreate(id, deploymentID, template string, dryRun bool) error {
	props := map[string]interface{}{
		"hmcVersion":   build.Version,
		"deploymentID": deploymentID,
		"template":     template,
		"dryRun":       dryRun,
	}
	return TrackEvent(deploymentCreateEvent, id, props)
}

func TrackEvent(name, id string, properties map[string]interface{}) error {
	if client == nil {
		return nil
	}
	return client.Enqueue(analytics.Track{
		AnonymousId: id,
		Event:       name,
		Properties:  properties,
	})
}
