package main

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
)

type Interface struct {
	Id    int64
	Name  string
	addrs []*InterfaceAddress
}

func (i *Interface) Addresses() []*InterfaceAddress {
	return i.addrs
}

type Route struct {
	iface    *Interface
	Src      string
	Dst      string
	Priority uint32
}

type InterfaceAddressSelector func([]*InterfaceAddress, net.IP, net.IP) *InterfaceAddress

func (*Route) Selector() InterfaceAddressSelector {
	return FirstAddressSelector
}

func (r *Route) Interface() (*Interface, error) {
	return r.iface, nil
}
func (r *Route) SrcNet() *net.IPNet {
	_, n, _ := net.ParseCIDR(r.Src)
	return n
}
func (r *Route) DstNet() *net.IPNet {
	_, n, _ := net.ParseCIDR(r.Dst)
	return n
}

func FirstAddressSelector(a []*InterfaceAddress, src, dst net.IP) *InterfaceAddress {
	if len(a) > 0 {
		return a[0]
	}
	return nil
}

type InterfaceAddress struct {
	IP        net.IP
	Netmask   net.IPMask
	Broadaddr net.IP
	Gateway   net.IP
}

type Router struct {
	ifaces map[int64]*Interface
	v4, v6 routeSlice
}

func NewRouter() *Router {
	return &Router{
		ifaces: make(map[int64]*Interface),
	}
}

func (r *Router) V4Route() []*RTInfo {
	return r.v4
}
func (r *Router) V6Route() []*RTInfo {
	return r.v6
}

func (r *Router) Interfaces() map[int64]*Interface {
	return r.ifaces
}

func (r *Router) AddRoutes(priority uint32, routes ...*Route) {
	for _, route := range routes {
		iface, err := route.Interface()
		if err != nil {
			continue
		}
		r.ifaces[iface.Id] = iface
		rt := &RTInfo{
			Src:      route.SrcNet(),
			Dst:      route.DstNet(),
			Selector: route.Selector(),
			Priority: route.Priority + priority,
			Iface:    iface.Id,
		}
		if len(route.DstNet().IP) == net.IPv4len {
			r.v4 = append(r.v4, rt)
		} else if len(route.DstNet().IP) == net.IPv6len {
			r.v6 = append(r.v6, rt)
		}
	}
}
func (r *Router) Update() {
	sort.Sort(r.v4)
	sort.Sort(r.v6)
}

func (r *Router) String() string {
	strs := []string{"ROUTER", "--- V4 ---"}
	for _, route := range r.v4 {
		strs = append(strs, fmt.Sprintf("%+v", *route))
	}
	strs = append(strs, "--- V6 ---")
	for _, route := range r.v6 {
		strs = append(strs, fmt.Sprintf("%+v", *route))
	}
	return strings.Join(strs, "\n")
}

func (r *Router) RouteWithSrc(src, dst net.IP) (iface *Interface, preferredSrc *InterfaceAddress, err error) {
	var rt *RTInfo
	switch {
	case dst.To4() != nil:
		rt, err = r.route(r.v4, src, dst)
	case dst.To16() != nil:
		rt, err = r.route(r.v6, src, dst)
	default:
		err = errors.New("IP is not valid as IPv4 or IPv6")
	}

	if err != nil {
		return
	}
	iface = r.ifaces[rt.Iface]

	var selector InterfaceAddressSelector = FirstAddressSelector
	if rt.Selector != nil {
		selector = rt.Selector
	}
	return iface, selector(iface.Addresses(), src, dst), nil
}

func (r *Router) route(routes routeSlice, src, dst net.IP) (rt *RTInfo, err error) {
	for _, rt = range routes {
		if rt.Src != nil && !rt.Src.Contains(src) {
			continue
		}
		if rt.Dst != nil && !rt.Dst.Contains(dst) {
			continue
		}
		return
	}
	err = fmt.Errorf("no route found for %v", dst)
	return
}

type RTInfo struct {
	Src, Dst *net.IPNet
	Selector InterfaceAddressSelector
	Priority uint32
	Iface    int64
}

type routeSlice []*RTInfo

func (r routeSlice) Len() int {
	return len(r)
}
func (r routeSlice) Less(i, j int) bool {
	iSize, _ := r[i].Dst.Mask.Size()
	jSize, _ := r[j].Dst.Mask.Size()
	if iSize != jSize {
		return jSize < iSize // large first
	}
	return r[i].Priority < r[j].Priority
}
func (r routeSlice) Swap(i, j int) {
	r[i], r[j] = r[j], r[i]
}

func main() {
	//初始化路由器
	router := NewRouter()
	//初始化路由表
	iface1 := &Interface{
		Id:   0,
		Name: "eth0",
		addrs: []*InterfaceAddress{
			&InterfaceAddress{
				IP:        net.ParseIP("192.168.1.2"),
				Gateway:   net.ParseIP("192.168.1.1"),
				Netmask:   net.CIDRMask(24, 32),
				Broadaddr: net.ParseIP("192.168.1.255"),
			},
			&InterfaceAddress{
				IP:        net.ParseIP("192.168.1.3"),
				Gateway:   net.ParseIP("192.168.1.1"),
				Netmask:   net.CIDRMask(24, 32),
				Broadaddr: net.ParseIP("192.168.1.255"),
			},
		},
	}

	iface2 := &Interface{
		Id:   1,
		Name: "eth1",
		addrs: []*InterfaceAddress{
			&InterfaceAddress{
				IP:        net.ParseIP("10.0.0.2"),
				Gateway:   net.ParseIP("10.0.0.1"),
				Netmask:   net.CIDRMask(8, 32),
				Broadaddr: net.ParseIP("10.255.255.255"),
			},
		},
	}
	//设置路由
	rt := []*Route{
		&Route{
			iface:    iface1,
			Dst:      "0.0.0.0/0",
			Src:      "0.0.0.0/0",
			Priority: 0,
		},
		&Route{
			iface:    iface1,
			Dst:      "172.16.1.0/24",
			Src:      "0.0.0.0/0",
			Priority: 0,
		},
		&Route{
			iface:    iface2,
			Dst:      "172.16.1.0/26",
			Src:      "0.0.0.0/0",
			Priority: 0,
		},
		&Route{
			iface:    iface2,
			Dst:      "172.16.2.0/24",
			Src:      "0.0.0.0/0",
			Priority: 0,
		},
		&Route{
			iface:    iface2,
			Dst:      "172.16.3.0/24",
			Src:      "0.0.0.0/0",
			Priority: 0,
		},
	}
	router.AddRoutes(0, rt...)
	router.Update()
	fmt.Println(router.String())

	fmt.Println("-- TESTING --")

	//从192.168.1.2到IP 223.5.5.5
	iface, addr, _ := router.RouteWithSrc(net.ParseIP("192.168.1.2"), net.ParseIP("223.5.5.5"))
	fmt.Printf("to 223.5.5.5, VIA %#s, Next: %#s\n", iface.Name, addr.Gateway.String())

	//从192.168.1.2到172.16.1.100
	iface, addr, _ = router.RouteWithSrc(net.ParseIP("192.168.1.2"), net.ParseIP("172.16.1.100"))
	fmt.Printf("to 172.16.1.100, VIA %#s, Next: %#s\n", iface.Name, addr.Gateway.String())

	//从192.168.1.2到172.16.1.10
	iface, addr, _ = router.RouteWithSrc(net.ParseIP("192.168.1.2"), net.ParseIP("172.16.1.10"))
	fmt.Printf("to 172.16.1.10, VIA %#s, Next: %#s\n", iface.Name, addr.Gateway.String())

	//从192.168.1.2到172.16.2.100
	iface, addr, _ = router.RouteWithSrc(net.ParseIP("192.168.1.2"), net.ParseIP("172.16.2.100"))
	fmt.Printf("to 172.16.2.100, VIA %#s, Next: %#s\n", iface.Name, addr.Gateway.String())

	//从192.168.1.3到172.16.2.100
	iface, addr, _ = router.RouteWithSrc(net.ParseIP("192.168.1.2"), net.ParseIP("172.16.3.100"))
	fmt.Printf("to 172.16.3.100, VIA %#s, Next: %#s\n", iface.Name, addr.Gateway.String())
}
