// Copyright 2016 VMware, Inc. All Rights Reserved.
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

package tether

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/d2g/dhcp4"
	"github.com/vishvananda/netlink"

	"github.com/vmware/vic/lib/dhcp"
	"github.com/vmware/vic/lib/dhcp/client"
	"github.com/vmware/vic/pkg/ip"
	"github.com/vmware/vic/pkg/trace"
	"github.com/vmware/vmw-guestinfo/rpcout"
)

var (
	hostnameFile = "/etc/hostname"
	byLabelDir   = "/dev/disk/by-label"
)

const (
	pciDevPath = "/sys/bus/pci/devices"
)

type BaseOperations struct {
	dhcpLoops    map[string]chan struct{}
	dynEndpoints map[string][]*NetworkEndpoint
	config       Config
}

// NetLink gives us an interface to the netlink calls used so that
// we can test the calling code.
type Netlink interface {
	LinkByName(string) (netlink.Link, error)
	LinkSetName(netlink.Link, string) error
	LinkSetDown(netlink.Link) error
	LinkSetUp(netlink.Link) error
	LinkSetAlias(netlink.Link, string) error
	AddrList(netlink.Link, int) ([]netlink.Addr, error)
	AddrAdd(netlink.Link, *netlink.Addr) error
	AddrDel(netlink.Link, *netlink.Addr) error
	RouteAdd(*netlink.Route) error
	RouteDel(*netlink.Route) error
	// Not quite netlink, but tightly associated

	LinkBySlot(slot int32) (netlink.Link, error)
}

func (t *BaseOperations) LinkByName(name string) (netlink.Link, error) {
	return netlink.LinkByName(name)
}

func (t *BaseOperations) LinkSetName(link netlink.Link, name string) error {
	return netlink.LinkSetName(link, name)
}

func (t *BaseOperations) LinkSetDown(link netlink.Link) error {
	return netlink.LinkSetDown(link)
}

func (t *BaseOperations) LinkSetUp(link netlink.Link) error {
	return netlink.LinkSetUp(link)
}

func (t *BaseOperations) LinkSetAlias(link netlink.Link, alias string) error {
	return netlink.LinkSetAlias(link, alias)
}

func (t *BaseOperations) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return netlink.AddrList(link, family)
}

func (t *BaseOperations) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrAdd(link, addr)
}

func (t *BaseOperations) AddrDel(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrDel(link, addr)
}

func (t *BaseOperations) RouteAdd(route *netlink.Route) error {
	return netlink.RouteAdd(route)
}

func (t *BaseOperations) RouteDel(route *netlink.Route) error {
	return netlink.RouteDel(route)
}

func (t *BaseOperations) LinkBySlot(slot int32) (netlink.Link, error) {
	pciPath, err := slotToPCIPath(slot)
	if err != nil {
		return nil, err
	}

	name, err := pciToLinkName(pciPath)
	if err != nil {
		return nil, err
	}

	log.Debugf("got link name: %#v", name)
	return t.LinkByName(name)
}

// SetHostname sets both the kernel hostname and /etc/hostname to the specified string
func (t *BaseOperations) SetHostname(hostname string, aliases ...string) error {
	defer trace.End(trace.Begin("setting hostname to " + hostname))

	old, err := os.Hostname()
	if err != nil {
		log.Warnf("Unable to get current hostname - will not be able to revert on failure: %s", err)
	}

	err = Sys.Syscall.Sethostname([]byte(hostname))
	if err != nil {
		log.Errorf("Unable to set hostname: %s", err)
		return err
	}
	log.Debugf("Updated kernel hostname")

	// update /etc/hostname to match
	err = ioutil.WriteFile(hostnameFile, []byte(hostname), 0644)
	if err != nil {
		log.Errorf("Failed to update hostname in %s", hostnameFile)

		// revert the hostname
		if old != "" {
			log.Warnf("Reverting kernel hostname to %s", old)
			err2 := Sys.Syscall.Sethostname([]byte(old))
			if err2 != nil {
				log.Errorf("Unable to revert kernel hostname - kernel and hostname file are out of sync! Error: %s", err2)
			}
		}

		return err
	}

	// add entry to hosts for resolution without nameservers
	lo4 := net.IPv4(127, 0, 1, 1)
	for _, a := range append(aliases, hostname) {
		Sys.Hosts.SetHost(a, lo4)
	}
	if err = Sys.Hosts.Save(); err != nil {
		return err
	}

	return nil
}

func slotToPCIPath(pciSlot int32) (string, error) {
	// see https://kb.vmware.com/kb/2047927
	dev := pciSlot & 0x1f
	bus := (pciSlot >> 5) & 0x1f
	fun := (pciSlot >> 10) & 0x7
	if bus == 0 {
		return path.Join(pciDevPath, fmt.Sprintf("0000:%02x:%02x.%d", bus, dev, fun)), nil
	}

	// device on secondary bus, prepend pci bridge address
	bridgeSlot := 0x11 + (bus - 1)
	bridgeAddr, err := slotToPCIPath(bridgeSlot)
	if err != nil {
		return "", err
	}

	return path.Join(bridgeAddr, fmt.Sprintf("0000:*:%02x.%d", dev, fun)), nil
}

func pciToLinkName(pciPath string) (string, error) {
	p := filepath.Join(pciPath, "net", "*")
	matches, err := filepath.Glob(p)
	if err != nil {
		return "", err
	}

	if len(matches) != 1 {
		return "", fmt.Errorf("%d eth interfaces match %s (%v)", len(matches), p, matches)
	}

	return path.Base(matches[0]), nil
}

func renameLink(t Netlink, link netlink.Link, slot int32, endpoint *NetworkEndpoint) (rlink netlink.Link, err error) {
	rlink = link
	defer func() {
		// we still need to ensure that the link is up irrespective of path
		err = t.LinkSetUp(link)
		if err != nil {
			err = fmt.Errorf("failed to bring link %s up: %s", link.Attrs().Name, err)
		}
	}()

	if link.Attrs().Name == endpoint.Name || link.Attrs().Alias == endpoint.Name || endpoint.Name == "" {
		// if the network is already identified, whether as primary name or alias it doesn't need repeating.
		// if the name is empty then it should not be aliases or named directly. IPAM data should still be applied.
		return link, nil
	}

	if strings.HasPrefix(link.Attrs().Name, "eth") {
		log.Infof("Renaming link %s to %s", link.Attrs().Name, endpoint.Name)

		err := t.LinkSetDown(link)
		if err != nil {
			detail := fmt.Sprintf("failed to set link %s down for rename: %s", link.Attrs().Name, err)
			return nil, errors.New(detail)
		}

		err = t.LinkSetName(link, endpoint.Name)
		if err != nil {
			return nil, err
		}

		// reacquire link with updated attributes
		link, err := t.LinkBySlot(slot)
		if err != nil {
			detail := fmt.Sprintf("unable to reacquire link %s after rename pass: %s", link.Attrs().Name, err)
			return nil, errors.New(detail)
		}

		return link, nil
	}

	if link.Attrs().Alias == "" {
		log.Infof("Aliasing link %s to %s", link.Attrs().Name, endpoint.Name)
		err := t.LinkSetAlias(link, endpoint.Name)
		if err != nil {
			return nil, err
		}

		// reacquire link with updated attributes
		link, err := t.LinkBySlot(slot)
		if err != nil {
			detail := fmt.Sprintf("unable to reacquire link %s after rename pass: %s", link.Attrs().Name, err)
			return nil, errors.New(detail)
		}

		return link, nil
	}

	log.Warnf("Unable to add additional alias on link %s for %s", link.Attrs().Name, endpoint.Name)
	return link, nil
}

func getDynamicIP(t Netlink, link netlink.Link, endpoint *NetworkEndpoint) (client.Client, error) {
	var ack *dhcp.Packet
	var err error

	// use dhcp to acquire address
	dc, err := client.NewClient(link.Attrs().Index, link.Attrs().HardwareAddr)
	if err != nil {
		return nil, err
	}

	params := []byte{byte(dhcp4.OptionSubnetMask)}
	if ip.IsUnspecifiedIP(endpoint.Network.Gateway.IP) {
		params = append(params, byte(dhcp4.OptionRouter))
	}
	if len(endpoint.Network.Nameservers) == 0 {
		params = append(params, byte(dhcp4.OptionDomainNameServer))
	}

	dc.SetParameterRequestList(params...)

	err = dc.Request()
	if err != nil {
		log.Errorf("error sending dhcp request: %s", err)
		return nil, err
	}

	ack = dc.LastAck()
	if ack.YourIP() == nil || ack.SubnetMask() == nil {
		err = fmt.Errorf("dhcp assigned nil ip or subnet mask")
		log.Error(err)
		return nil, err
	}

	log.Infof("DHCP response: IP=%s, SubnetMask=%s, Gateway=%s, DNS=%s, Lease Time=%s", ack.YourIP(), ack.SubnetMask(), ack.Gateway(), ack.DNS(), ack.LeaseTime())
	defer func() {
		if err != nil && ack != nil {
			dc.Release()
		}
	}()

	return dc, nil
}

func updateEndpoint(newIP *net.IPNet, endpoint *NetworkEndpoint) {
	log.Debugf("updateEndpoint(%s, %+v)", newIP, endpoint)

	dhcp := endpoint.DHCP
	if dhcp == nil {
		endpoint.Assigned = *newIP
		endpoint.Network.Assigned.Gateway = endpoint.Network.Gateway
		endpoint.Network.Assigned.Nameservers = endpoint.Network.Nameservers
		return
	}

	endpoint.Assigned = dhcp.Assigned
	endpoint.Network.Assigned.Gateway = dhcp.Gateway
	if len(dhcp.Nameservers) > 0 {
		endpoint.Network.Assigned.Nameservers = dhcp.Nameservers
	}
}

func linkAddrUpdate(old, new *net.IPNet, t Netlink, link netlink.Link) error {
	log.Infof("setting ip address %s for link %s", new, link.Attrs().Name)

	if old != nil && !old.IP.Equal(new.IP) {
		log.Debugf("removing old address %s", old)
		if err := t.AddrDel(link, &netlink.Addr{IPNet: old}); err != nil {
			if errno, ok := err.(syscall.Errno); !ok || errno != syscall.EADDRNOTAVAIL {
				log.Errorf("failed to remove existing address %s: %s", old, err)
				return err
			}
		}

		log.Debugf("removed old address %s for link %s", old, link.Attrs().Name)
	}

	// assign IP to NIC
	if err := t.AddrAdd(link, &netlink.Addr{IPNet: new}); err != nil {
		if errno, ok := err.(syscall.Errno); !ok || errno != syscall.EEXIST {
			log.Errorf("failed to assign ip %s for link %s", new, link.Attrs().Name)
			return err

		}

		log.Warnf("address %s already set on interface %s", new, link.Attrs().Name)
	}

	log.Debugf("added address %s to link %s", new, link.Attrs().Name)
	return nil
}

func updateRoutes(t Netlink, link netlink.Link, endpoint *NetworkEndpoint) error {
	gw := endpoint.Network.Assigned.Gateway
	if ip.IsUnspecifiedIP(gw.IP) {
		return nil
	}

	if endpoint.Network.Default {
		return updateDefaultRoute(t, link, endpoint)
	}

	for _, d := range endpoint.Network.Destinations {
		r := &netlink.Route{
			LinkIndex: link.Attrs().Index,
			Dst:       &d,
			Gw:        gw.IP,
		}

		if err := t.RouteAdd(r); err != nil && !os.IsNotExist(err) {
			log.Errorf("failed to add route for destination %s via gateway %s", d, gw.IP)
		}
	}

	return nil
}

func updateDefaultRoute(t Netlink, link netlink.Link, endpoint *NetworkEndpoint) error {
	gw := endpoint.Network.Assigned.Gateway
	// Add routes
	if !endpoint.Network.Default || ip.IsUnspecifiedIP(gw.IP) {
		log.Debugf("not setting route for network: default=%v gateway=%s", endpoint.Network.Default, gw.IP)
		return nil
	}

	_, defaultNet, _ := net.ParseCIDR("0.0.0.0/0")
	// delete default route first
	if err := t.RouteDel(&netlink.Route{LinkIndex: link.Attrs().Index, Dst: defaultNet}); err != nil {
		if errno, ok := err.(syscall.Errno); !ok || errno != syscall.ESRCH {
			return fmt.Errorf("could not update default route: %s", err)
		}
	}

	log.Infof("Setting default gateway to %s", gw.IP)
	route := &netlink.Route{LinkIndex: link.Attrs().Index, Dst: defaultNet, Gw: gw.IP}
	if err := t.RouteAdd(route); err != nil {
		return fmt.Errorf("failed to add gateway route for endpoint %s: %s", endpoint.Network.Name, err)
	}

	log.Infof("updated default route to %s interface, gateway: %s", endpoint.Network.Name, gw.IP)
	return nil
}

func (t *BaseOperations) updateHosts(endpoint *NetworkEndpoint) error {
	log.Debugf("%+v", endpoint)
	// Add /etc/hosts entry
	if endpoint.Network.Name == "" {
		return nil
	}

	Sys.Hosts.SetHost(fmt.Sprintf("%s.localhost", endpoint.Network.Name), endpoint.Assigned.IP)

	if err := Sys.Hosts.Save(); err != nil {
		return err
	}

	return nil
}

func (t *BaseOperations) updateNameservers(endpoint *NetworkEndpoint) error {
	ns := endpoint.Network.Assigned.Nameservers
	gw := endpoint.Network.Assigned.Gateway
	// Add nameservers
	// This is incredibly trivial for now - should be updated to a less messy approach
	if len(ns) > 0 {
		Sys.ResolvConf.AddNameservers(ns...)
		log.Infof("Added nameservers: %+v", ns)
	} else if !ip.IsUnspecifiedIP(gw.IP) {
		Sys.ResolvConf.AddNameservers(gw.IP)
		log.Infof("Added nameserver: %s", gw.IP)
	}

	if err := Sys.ResolvConf.Save(); err != nil {
		return err
	}

	return nil
}

func (t *BaseOperations) Apply(endpoint *NetworkEndpoint) error {
	defer trace.End(trace.Begin("applying endpoint configuration for " + endpoint.Network.Name))

	return apply(t, t, endpoint)
}

func apply(nl Netlink, t *BaseOperations, endpoint *NetworkEndpoint) error {
	if endpoint.applied {
		log.Infof("skipping applying config for network %s as it has been applied already", endpoint.Network.Name)
		return nil // already applied
	}

	// Locate interface
	slot, err := strconv.Atoi(endpoint.ID)
	if err != nil {
		return fmt.Errorf("endpoint ID must be a base10 numeric pci slot identifier: %s", err)
	}

	defer func() {
		if err == nil {
			log.Infof("successfully applied config for network %s", endpoint.Network.Name)
			endpoint.applied = true
		}
	}()

	var link netlink.Link
	link, err = nl.LinkBySlot(int32(slot))
	if err != nil {
		return fmt.Errorf("unable to acquire reference to link %s: %s", endpoint.ID, err)
	}

	// rename the link if needed
	link, err = renameLink(nl, link, int32(slot), endpoint)
	if err != nil {
		return fmt.Errorf("unable to reacquire link %s after rename pass: %s", endpoint.ID, err)
	}

	var dc client.Client
	defer func() {
		if err != nil && dc != nil {
			dc.Release()
		}
	}()

	var newIP *net.IPNet

	if endpoint.IsDynamic() && endpoint.DHCP == nil {
		if e, ok := t.dynEndpoints[endpoint.ID]; ok {
			// endpoint shares NIC, copy over DHCP
			endpoint.DHCP = e[0].DHCP
		}
	}

	log.Debugf("%+v", endpoint)
	if endpoint.IsDynamic() {
		if endpoint.DHCP == nil {
			dc, err = getDynamicIP(nl, link, endpoint)
			if err != nil {
				return err
			}

			ack := dc.LastAck()
			endpoint.DHCP = &DHCPInfo{
				Assigned:    net.IPNet{IP: ack.YourIP(), Mask: ack.SubnetMask()},
				Nameservers: ack.DNS(),
				Gateway:     net.IPNet{IP: ack.Gateway(), Mask: ack.SubnetMask()},
			}
		}
		newIP = &endpoint.DHCP.Assigned
	} else {
		newIP = endpoint.IP
		if newIP.IP.Equal(net.IPv4zero) {
			// managed externally
			return nil
		}
	}

	var old *net.IPNet
	if !ip.IsUnspecifiedIP(endpoint.Assigned.IP) {
		old = &endpoint.Assigned
	}

	if err = linkAddrUpdate(old, newIP, nl, link); err != nil {
		return err
	}

	updateEndpoint(newIP, endpoint)

	if err = updateRoutes(nl, link, endpoint); err != nil {
		return err
	}

	if err = t.updateHosts(endpoint); err != nil {
		return err
	}

	Sys.ResolvConf.RemoveNameservers(endpoint.Network.Assigned.Nameservers...)
	if err = t.updateNameservers(endpoint); err != nil {
		return err
	}

	if endpoint.IsDynamic() {
		eps := t.dynEndpoints[endpoint.ID]
		found := false
		for _, e := range eps {
			if e == endpoint {
				found = true
				break
			}
		}

		if !found {
			eps = append(eps, endpoint)
			t.dynEndpoints[endpoint.ID] = eps
		}
	}

	// add renew/release loop if necessary
	if dc != nil {
		if _, ok := t.dhcpLoops[endpoint.ID]; !ok {
			stop := make(chan struct{})
			if err != nil {
				log.Errorf("could not make DHCP client id for link %s: %s", link.Attrs().Name, err)
			} else {
				t.dhcpLoops[endpoint.ID] = stop
				go t.dhcpLoop(stop, endpoint, dc)
			}
		}
	}

	return nil
}

func (t *BaseOperations) dhcpLoop(stop chan struct{}, e *NetworkEndpoint, dc client.Client) {
	exp := time.After(dc.LastAck().LeaseTime() / 2)
	for {
		select {
		case <-stop:
			// release the ip
			log.Infof("releasing IP address for network %s", e.Name)
			dc.Release()
			return

		case <-exp:
			log.Infof("renewing IP address for network %s", e.Name)
			err := dc.Renew()
			if err != nil {
				log.Errorf("failed to renew ip address for network %s: %s", e.Name, err)
				continue
			}

			ack := dc.LastAck()
			log.Infof("successfully renewed ip address: IP=%s, SubnetMask=%s, Gateway=%s, DNS=%s, Lease Time=%s", ack.YourIP(), ack.SubnetMask(), ack.Gateway(), ack.DNS(), ack.LeaseTime())

			e.DHCP = &DHCPInfo{
				Assigned:    net.IPNet{IP: ack.YourIP(), Mask: ack.SubnetMask()},
				Gateway:     net.IPNet{IP: ack.Gateway(), Mask: ack.SubnetMask()},
				Nameservers: ack.DNS(),
			}

			t.Apply(e)
			if err = t.config.UpdateNetworkEndpoint(e); err != nil {
				log.Error(err)
			}
			// update any endpoints that share this NIC
			for _, d := range t.dynEndpoints[e.ID] {
				if e == d {
					continue
				}

				d.DHCP = e.DHCP
				t.Apply(d)
				if err = t.config.UpdateNetworkEndpoint(d); err != nil {
					log.Error(err)
				}
			}

			t.config.Flush()

			exp = time.After(ack.LeaseTime() / 2)
		}
	}
}

// MountLabel performs a mount with the label and target being absolute paths
func (t *BaseOperations) MountLabel(ctx context.Context, label, target string) error {
	defer trace.End(trace.Begin(fmt.Sprintf("Mounting %s on %s", label, target)))

	if err := os.MkdirAll(target, 0600); err != nil {
		return fmt.Errorf("unable to create mount point %s: %s", target, err)
	}

	// convert the label to a filesystem path
	label = "/dev/disk/by-label/" + label

	// do..while ! timedout
	var timeout bool
	for timeout = false; !timeout; {
		_, err := os.Stat(label)
		if err == nil || !os.IsNotExist(err) {
			break
		}

		deadline, ok := ctx.Deadline()
		timeout = ok && time.Now().After(deadline)
	}

	if timeout {
		detail := fmt.Sprintf("timed out waiting for %s to appear", label)
		return errors.New(detail)
	}

	if err := Sys.Syscall.Mount(label, target, "ext4", syscall.MS_NOATIME, ""); err != nil {
		detail := fmt.Sprintf("mounting %s on %s failed: %s", label, target, err)
		return errors.New(detail)
	}

	return nil
}

// ProcessEnv does OS specific checking and munging on the process environment prior to launch
func (t *BaseOperations) ProcessEnv(env []string) []string {
	// TODO: figure out how we're going to specify user and pass all the settings along
	// in the meantime, hardcode HOME to /root
	homeIndex := -1
	for i, tuple := range env {
		if strings.HasPrefix(tuple, "HOME=") {
			homeIndex = i
			break
		}
	}
	if homeIndex == -1 {
		return append(env, "HOME=/root")
	}

	return env
}

// Fork triggers vmfork and handles the necessary pre/post OS level operations
func (t *BaseOperations) Fork() error {
	// unload vmxnet3 module

	// fork
	out, ok, err := rpcout.SendOne("vmfork-begin -1 -1")
	if err != nil {
		detail := fmt.Sprintf("error while calling vmfork: err=%s, out=%s, ok=%t", err, out, ok)
		log.Error(detail)
		return errors.New(detail)
	}

	if !ok {
		detail := fmt.Sprintf("failed to vmfork: %s", out)
		log.Error(detail)
		return errors.New(detail)
	}

	log.Infof("vmfork call succeeded: %s", out)

	// update system time

	// rescan scsi bus

	// reload vmxnet3 module

	// ensure memory and cores are brought online if not using udev

	return nil
}

func (t *BaseOperations) Setup(config Config) error {
	err := Sys.Hosts.Load()
	if err != nil {
		return err
	}

	// make sure localhost entries are present
	entries := []struct {
		hostname string
		addr     net.IP
	}{
		{"localhost", net.ParseIP("127.0.0.1")},
		{"ip6-localhost", net.ParseIP("::1")},
		{"ip6-loopback", net.ParseIP("::1")},
		{"ip6-localnet", net.ParseIP("fe00::0")},
		{"ip6-mcastprefix", net.ParseIP("ff00::0")},
		{"ip6-allnodes", net.ParseIP("ff02::1")},
		{"ip6-allrouters", net.ParseIP("ff02::2")},
	}

	for _, e := range entries {
		Sys.Hosts.SetHost(e.hostname, e.addr)
	}

	if err = Sys.Hosts.Save(); err != nil {
		return err
	}

	t.dynEndpoints = make(map[string][]*NetworkEndpoint)
	t.dhcpLoops = make(map[string]chan struct{})
	t.config = config

	return nil
}

func (t *BaseOperations) Cleanup() error {
	for _, stop := range t.dhcpLoops {
		close(stop)
	}

	return nil
}

// Need to put this here because Windows does not
// support SysProcAttr.Credential
func getUserSysProcAttr(uname string) *syscall.SysProcAttr {
	uinfo, err := user.Lookup(uname)
	if err != nil {
		detail := fmt.Sprintf("Unable to find user %s: %s", uname, err)
		log.Error(detail)
		return nil
	} else {
		u, _ := strconv.Atoi(uinfo.Uid)
		g, _ := strconv.Atoi(uinfo.Gid)
		// Unfortunately lookup GID by name is currently unsupported in Go.
		return &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid: uint32(u),
				Gid: uint32(g),
			},
			Setsid: true,
		}
	}
}
