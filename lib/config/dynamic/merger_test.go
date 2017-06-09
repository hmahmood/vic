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
package dynamic

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/vmware/vic/pkg/registry"
)

func TestWhitelistMerger(t *testing.T) {
	var tests = []struct {
		orig, other registry.Entry
		res         registry.Entry
		err         error
	}{
		{
			orig:  registry.ParseEntry("10.10.10.10"),
			other: registry.ParseEntry("10.10.10.10"),
			res:   registry.ParseEntry("10.10.10.10"),
		},
		{
			orig:  registry.ParseEntry("10.10.10.10"),
			other: registry.ParseEntry("10.10.10.20"),
		},
		{
			orig:  registry.ParseEntry("10.10.10.10/24"),
			other: registry.ParseEntry("10.10.10.10/24"),
			res:   registry.ParseEntry("10.10.10.10/24"),
		},
		{
			other: registry.ParseEntry("10.10.10.10/24"),
			orig:  registry.ParseEntry("192.168.1.0/24"),
		},
		{
			orig:  registry.ParseEntry("10.10.10.10/24"),
			other: registry.ParseEntry("10.10.10.10/16"),
			err:   assert.AnError,
		},
		{
			orig:  registry.ParseEntry("10.10.10.10/16"),
			other: registry.ParseEntry("10.10.10.10/24"),
			res:   registry.ParseEntry("10.10.10.10/24"),
		},
		{
			orig:  registry.ParseEntry("*.google.com"),
			other: registry.ParseEntry("*.google.com"),
			res:   registry.ParseEntry("*.google.com"),
		},
		{
			orig:  registry.ParseEntry("*.yahoo.com"),
			other: registry.ParseEntry("*.google.com"),
		},
		{
			orig:  registry.ParseEntry("*.google.com"),
			other: registry.ParseEntry("mail.google.com"),
			res:   registry.ParseEntry("mail.google.com"),
		},
		{
			orig:  registry.ParseEntry("mail.google.com"),
			other: registry.ParseEntry("*.google.com"),
			err:   assert.AnError,
		},
		{
			orig:  registry.ParseEntry("192.168.1.1:123"),
			other: registry.ParseEntry("192.168.1.1"),
		},
		{
			orig:  registry.ParseEntry("192.168.1.1"),
			other: registry.ParseEntry("192.168.1.1:123"),
		},
		{
			orig:  registry.ParseEntry("foo:123"),
			other: registry.ParseEntry("foo"),
		},
		{
			orig:  registry.ParseEntry("foo"),
			other: registry.ParseEntry("foo:123"),
		},
		{
			orig:  registry.ParseEntry("http://foo"),
			other: registry.ParseEntry("foo:123"),
		},
		{
			orig:  registry.ParseEntry("http://foo"),
			other: registry.ParseEntry("http://foo:123"),
		},
		{
			orig:  registry.ParseEntry("http://foo:123"),
			other: registry.ParseEntry("http://foo"),
		},
		{
			orig:  registry.ParseEntry("http://foo/bar"),
			other: registry.ParseEntry("http://foo"),
			err:   assert.AnError,
		},
		{
			orig:  registry.ParseEntry("https://foo/bar"),
			other: registry.ParseEntry("http://foo/bar"),
		},
		{
			orig:  registry.ParseEntry("https://foo"),
			other: registry.ParseEntry("foo"),
		},
	}

	m := &whitelistMerger{}

	for _, te := range tests {
		res, err := m.Merge(te.orig, te.other)
		if te.err == nil {
			assert.Nil(t, err, "case: orig: %s, other: %s, err: %v, res: %s", te.orig, te.other, te.err, te.res)
		} else {
			assert.NotNil(t, err, "case: orig: %s, other: %s, err: %v, res: %s", te.orig, te.other, te.err, te.res)
		}

		if te.res == nil {
			assert.Nil(t, res, "case: orig: %s, other: %s, err: %v, res: %s", te.orig, te.other, te.err, te.res)
		} else {
			assert.True(t, te.res.Equal(res), "%s merge %s, %s (expected) == %s (actual)", te.orig, te.other, te.res, res)
		}
	}
}
