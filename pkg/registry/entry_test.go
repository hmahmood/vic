// Copyright 2017 VMware, Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package registry

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEntryContains(t *testing.T) {
	var tests = []struct {
		first, second Entry
		res           bool
	}{
		{
			first:  ParseEntry("192.168.0.1"),
			second: ParseEntry("192.168.0.1"),
			res:    true,
		},
		{
			first:  ParseEntry("192.168.0.1"),
			second: ParseEntry("192.168.0.1/16"),
			res:    false,
		},
		{
			first:  ParseEntry("192.168.0.1"),
			second: ParseEntry("192.168.0.2"),
			res:    false,
		},
		{
			first:  ParseEntry("192.168.0.1/24"),
			second: ParseEntry("192.168.0.11"),
			res:    true,
		},
		{
			first:  ParseEntry("172.16.0.0/12"),
			second: ParseEntry("172.17.0.0/24"),
			res:    true,
		},
		{
			first:  ParseEntry("172.16.0.0/12"),
			second: ParseEntry("172.15.0.0/24"),
			res:    false,
		},
		{
			first:  ParseEntry("172.16.0.0/12"),
			second: ParseEntry("*.google.com"),
			res:    false,
		},
		{
			first:  ParseEntry("192.168.0.1/24"),
			second: ParseEntry("192.168.1.0"),
			res:    false,
		},
		{
			first:  ParseEntry("192.168.0.1/24"),
			second: ParseEntry("192.168.0.1/24"),
			res:    true,
		},
		{
			first:  ParseEntry("*.google.com"),
			second: ParseEntry("*.com"),
			res:    false,
		},
		{
			first:  ParseEntry("mail.google.com"),
			second: ParseEntry("*.google.com"),
			res:    false,
		},
		{
			first:  ParseEntry("*.google.com"),
			second: ParseEntry("mail.google.com"),
			res:    true,
		},
		{
			first:  ParseEntry("*.com"),
			second: ParseEntry("*.google.com"),
			res:    true,
		},
		{
			first:  ParseEntry("192.168.1.1:123"),
			second: ParseEntry("192.168.1.1"),
			res:    false,
		},
		{
			first:  ParseEntry("foo:123"),
			second: ParseEntry("foo"),
			res:    false,
		},
		{
			first:  ParseEntry("foo"),
			second: ParseEntry("foo:123"),
			res:    true,
		},
		{
			first:  ParseEntry("192.168.1.1"),
			second: ParseEntry("192.168.1.1:123"),
			res:    true,
		},
	}

	for _, te := range tests {
		assert.Equal(t, te.res, te.first.Contains(te.second), "test: %s contains %s", te.first, te.second)
	}

}

func TestEntryMatch(t *testing.T) {
	var tests = []struct {
		e   Entry
		s   string
		res bool
	}{
		{
			e:   &strEntry{"192.168.0.1"},
			s:   "192.168.0.1",
			res: true,
		},
		{
			e:   ParseEntry("192.168.0.1"),
			s:   "192.168.0",
			res: false,
		},
		{
			e:   ParseEntry("192.168.0.1/24"),
			s:   "192.168.0.1",
			res: true,
		},
		{
			e:   ParseEntry("192.168.0.1/24"),
			s:   "192.168.1.1",
			res: false,
		},
		{
			e:   ParseEntry("192.168.0.1/24"),
			s:   "192.168.0.1/24",
			res: false,
		},
		{
			e:   ParseEntry("*.google.com"),
			s:   "mail.google.com",
			res: true,
		},
		{
			e:   ParseEntry("*.google.com"),
			s:   "mail.yahoo.com",
			res: false,
		},
		{
			e:   ParseEntry("*.google.com"),
			s:   "google.com",
			res: false,
		},
		{
			e:   ParseEntry("foo:123"),
			s:   "foo",
			res: false,
		},
		{
			e:   ParseEntry("foo"),
			s:   "foo:123",
			res: true,
		},
		{
			e:   ParseEntry("192.168.1.1"),
			s:   "192.168.1.1:123",
			res: true,
		},
	}

	for _, te := range tests {
		assert.Equal(t, te.res, te.e.Match(te.s), "test: %s match %s", te.e, te.s)
	}
}

func TestEntryEqual(t *testing.T) {
	var tests = []struct {
		e, other Entry
		res      bool
	}{
		{
			e:     ParseEntry("192.168.0.1"),
			other: ParseEntry("192.168.0.1"),
			res:   true,
		},
		{
			e:     ParseEntry("192.168.0.1"),
			other: ParseEntry("192.168.0.2"),
			res:   false,
		},
		{
			e:     ParseEntry("192.168.0.1"),
			other: ParseEntry("192.168.1.0/24"),
			res:   false,
		},
		{
			e:     ParseEntry("192.168.0.1"),
			other: ParseEntry("*.google.com"),
			res:   false,
		},
		{
			e:     ParseEntry("192.168.0.1/24"),
			other: ParseEntry("192.168.0.1/24"),
			res:   true,
		},
		{
			e:     ParseEntry("192.168.0.1/24"),
			other: ParseEntry("192.168.0.1/16"),
			res:   false,
		},
		{
			e:     ParseEntry("192.168.0.1/24"),
			other: ParseEntry("192.168.0.1"),
			res:   false,
		},
		{
			e:     ParseEntry("192.168.0.1/24"),
			other: ParseEntry("*.google.com"),
			res:   false,
		},
		{
			e:     ParseEntry("*.google.com"),
			other: ParseEntry("*.google.com"),
			res:   true,
		},
		{
			e:     ParseEntry("*.google.com"),
			other: ParseEntry("mail.google.com"),
			res:   false,
		},
		{
			e:     ParseEntry("*.google.com"),
			other: ParseEntry("*.yahoo.com"),
			res:   false,
		},
		{
			e:     ParseEntry("*.google.com"),
			other: ParseEntry("192.168.0.1"),
			res:   false,
		},
		{
			e:     ParseEntry("*.google.com"),
			other: ParseEntry("192.168.0.1/24"),
			res:   false,
		},
	}

	for _, te := range tests {
		assert.Equal(t, te.res, te.e.Equal(te.other), "test: %s equal %s", te.e, te.other)
	}
}

func TestParseEntry(t *testing.T) {
	var tests = []struct {
		s   string
		res Entry
	}{
		{
			s:   "192.168.0.1",
			res: ParseEntry("192.168.0.1"),
		},
		{
			s:   "192.168.0.1:80",
			res: ParseEntry("192.168.0.1:80"),
		},
		{
			s:   "192.168.0",
			res: ParseEntry("192.168.0"),
		},
		{
			s:   "192.168.0.1/24",
			res: &cidrEntry{ipnet: &net.IPNet{IP: net.ParseIP("192.168.0.0"), Mask: net.CIDRMask(24, 32)}},
		},
		{
			s:   "192.168.0/24",
			res: &strEntry{e: "192.168.0/24"},
		},
		{
			s:   "*.google.com",
			res: &strEntry{e: "*.google.com"},
		},
		{
			s:   "google.com:8080",
			res: &strEntry{e: "google.com:8080"},
		},
	}

	for _, te := range tests {
		assert.True(t, te.res.Equal(ParseEntry(te.s)), "ParseEntry(%s) != %s", te.s, te.res)
	}
}