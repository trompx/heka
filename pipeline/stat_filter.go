/***** BEGIN LICENSE BLOCK *****
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this file,
# You can obtain one at http://mozilla.org/MPL/2.0/.
#
# The Initial Developer of the Original Code is the Mozilla Foundation.
# Portions created by the Initial Developer are Copyright (C) 2012
# the Initial Developer. All Rights Reserved.
#
# Contributor(s):
#   Ben Bangert (bbangert@mozilla.com)
#   Mike Trinkala (trink@mozilla.com)
#   Rob Miller (rmiller@mozilla.com)
#
# ***** END LICENSE BLOCK *****/

package pipeline

import (
	"fmt"
	"github.com/mozilla-services/heka/message"
)

// Simple struct representing a single statsd-style metric value.
type metric struct {
	// Supports "Counter", "Timer", or "Gauge"
	Type_ string `toml:"type"`
	Name  string
	Value string
}

// Heka Filter plugin that can accept specific message types, extract data
// from those messages, and from that data generate statsd messages in a
// StatsdInput exactly as if a statsd message has come from a networked statsd
// client.
type StatFilter struct {
	metrics       map[string]metric
	statAccumName string
}

// StatFilter config struct.
type StatFilterConfig struct {
	// Set of metric templates this filter should use, keyed by arbitrary
	// metric id.
	Metric map[string]metric
	// Configured name of StatAccumInput plugin to which this filter should be
	// delivering its stats. Defaults to "StatsAccumInput".
	StatAccumName string
}

func (s *StatFilter) ConfigStruct() interface{} {
	return &StatFilterConfig{
		StatAccumName: "StatAccumInput",
	}
}

func (s *StatFilter) Init(config interface{}) (err error) {
	conf := config.(*StatFilterConfig)
	s.metrics = conf.Metric
	s.statAccumName = conf.StatAccumName
	return
}

// For each message, we first extract any match group captures, and then we
// add our own values for "Logger", "Hostname", "Type", and "Payload" as if
// they were captured values. We then iterate through all of this plugin's
// defined metrics, and for each one we use the captures to do string
// substitution on both the name and the payload. For example, a metric with
// the name "@Hostname.404s" would become a stat with the "@Hostname" replaced
// by the hostname from the received message.
func (s *StatFilter) Run(fr FilterRunner, h PluginHelper) (err error) {
	var (
		statAccumInput InputRunner
		statAccum      StatAccumulator
		ok             bool
	)
	if statAccumInput, ok = h.PipelineConfig().InputRunners[s.statAccumName]; !ok {
		return fmt.Errorf("No Input named: '%s'", s.statAccumName)
	}
	if statAccum, ok = statAccumInput.(StatAccumulator); !ok {
		return fmt.Errorf("Input '%s' is not a StatAccumulator", s.statAccumName)
	}

	var (
		pack     *PipelinePack
		captures map[string]string
		stat     Stat
	)

	inChan := fr.InChan()
	for plc := range inChan {
		pack = plc.Pack
		captures = plc.Captures
		if captures == nil {
			captures = make(map[string]string)
		}

		// Load existing fields into the set for replacement
		captures["Logger"] = pack.Message.GetLogger()
		captures["Hostname"] = pack.Message.GetHostname()
		captures["Type"] = pack.Message.GetType()
		captures["Payload"] = pack.Message.GetPayload()

		for _, field := range pack.Message.Fields {
			if field.GetValueType() == message.Field_STRING && len(field.ValueString) > 0 {
				captures[field.GetName()] = field.ValueString[0]
			}
		}

		// We matched, generate appropriate metrics
		for _, met := range s.metrics {
			stat.Bucket = InterpolateString(met.Name, captures)
			switch met.Type_ {
			case "Counter":
				stat.Modifier = ""
			case "Timer":
				stat.Modifier = "ms"
			case "Gauge":
				stat.Modifier = "g"
			}
			stat.Value = InterpolateString(met.Value, captures)
			if !statAccum.DropStat(stat) {
				fr.LogError(fmt.Errorf("Undelivered stat: %s", stat))
			}
		}
		pack.Recycle()
	}

	return
}
