package grpcurl

import (
	"context"
	"fmt"
	"github.com/SunMaybo/zero/common/zlog"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fullstorydev/grpcurl"
	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/grpcreflect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	insecurecreds "google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	reflectpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"

	// Register gzip compressor so compressed responses will work
	_ "google.golang.org/grpc/encoding/gzip"
	// Register xds so xds and xds-experimental resolver schemes work
	_ "google.golang.org/grpc/xds"
)

// Uses a file source as a fallback for resolving symbols and extensions, but
// only uses the reflection source for listing services
type compositeSource struct {
	reflection grpcurl.DescriptorSource
	file       grpcurl.DescriptorSource
}

func (cs compositeSource) ListServices() ([]string, error) {
	return cs.reflection.ListServices()
}

func (cs compositeSource) FindSymbol(fullyQualifiedName string) (desc.Descriptor, error) {
	d, err := cs.reflection.FindSymbol(fullyQualifiedName)
	if err == nil {
		return d, nil
	}
	return cs.file.FindSymbol(fullyQualifiedName)
}

func (cs compositeSource) AllExtensionsForType(typeName string) ([]*desc.FieldDescriptor, error) {
	exts, err := cs.reflection.AllExtensionsForType(typeName)
	if err != nil {
		// On error fall back to file source
		return cs.file.AllExtensionsForType(typeName)
	}
	// Track the tag numbers from the reflection source
	tags := make(map[int32]bool)
	for _, ext := range exts {
		tags[ext.GetNumber()] = true
	}
	fileExts, err := cs.file.AllExtensionsForType(typeName)
	if err != nil {
		return exts, nil
	}
	for _, ext := range fileExts {
		// Prioritize extensions found via reflection
		if !tags[ext.GetNumber()] {
			exts = append(exts, ext)
		}
	}
	return exts, nil
}

type GrpcSession struct {
	Services []string
	Methods  []*desc.MethodDescriptor
	AllFiles []*desc.FileDescriptor
	C        *grpc.ClientConn
}

func GrpcConnection(target string) *GrpcSession {

	ctx := context.Background()
	dialTime := 10 * time.Second
	dialCtx, cancel := context.WithTimeout(ctx, dialTime)
	defer cancel()
	var opts []grpc.DialOption

	var creds credentials.TransportCredentials
	network := "tcp"

	cc, err := dial(dialCtx, network, target, creds, true, opts...)
	if err != nil {
		zlog.S.Fatal(err, "Failed to dial target host %q", target)
	}

	var descSource grpcurl.DescriptorSource
	var refClient *grpcreflect.Client
	var fileSource grpcurl.DescriptorSource

	md := grpcurl.MetadataFromHeaders(nil)
	refCtx := metadata.NewOutgoingContext(ctx, md)
	refClient = grpcreflect.NewClient(refCtx, reflectpb.NewServerReflectionClient(cc))
	reflSource := grpcurl.DescriptorSourceFromServer(ctx, refClient)
	if fileSource != nil {
		descSource = compositeSource{reflSource, fileSource}
	} else {
		descSource = reflSource
	}

	services, err := descSource.ListServices()
	if err != nil {
		zlog.S.Fatal(err, "Failed to compute set of services to expose")
	}
	methods, err := getMethods(descSource, nil)
	if err != nil {
		zlog.S.Fatal(err, "Failed to compute set of methods to expose")
	}
	allFiles, err := grpcurl.GetAllFiles(descSource)
	if err != nil {
		zlog.S.Fatal(err, "Failed to enumerate all proto files")
	}
	// can go ahead and close reflection client now
	if refClient != nil {
		refClient.Reset()
		refClient = nil
	}
	return &GrpcSession{
		Services: services,
		Methods:  methods,
		AllFiles: allFiles,
		C:        cc,
	}

}

type svcConfig struct {
	includeService bool
	includeMethods map[string]struct{}
}

func getMethods(source grpcurl.DescriptorSource, configs map[string]*svcConfig) ([]*desc.MethodDescriptor, error) {
	allServices, err := source.ListServices()
	if err != nil {
		return nil, err
	}

	var descs []*desc.MethodDescriptor
	for _, svc := range allServices {
		if svc == "grpc.reflection.v1alpha.ServerReflection" {
			continue
		}
		d, err := source.FindSymbol(svc)
		if err != nil {
			return nil, err
		}
		sd, ok := d.(*desc.ServiceDescriptor)
		if !ok {
			return nil, fmt.Errorf("%s should be a service descriptor but instead is a %T", d.GetFullyQualifiedName(), d)
		}
		cfg := configs[svc]
		if cfg == nil && len(configs) != 0 {
			// not configured to expose this service
			continue
		}
		delete(configs, svc)
		for _, md := range sd.GetMethods() {
			if cfg == nil {
				descs = append(descs, md)
				continue
			}
			_, found := cfg.includeMethods[md.GetName()]
			delete(cfg.includeMethods, md.GetName())
			if found && cfg.includeService {
				zlog.S.Warnf("Service %s already configured, so -method %s is unnecessary", svc, md.GetName())
			}
			if found || cfg.includeService {
				descs = append(descs, md)
			}
		}
		if cfg != nil && len(cfg.includeMethods) > 0 {
			// configured methods not found
			methodNames := make([]string, 0, len(cfg.includeMethods))
			for m := range cfg.includeMethods {
				methodNames = append(methodNames, fmt.Sprintf("%s/%s", svc, m))
			}
			sort.Strings(methodNames)
			return nil, fmt.Errorf("configured methods not found: %s", strings.Join(methodNames, ", "))
		}
	}

	if len(configs) > 0 {
		// configured services not found
		svcNames := make([]string, 0, len(configs))
		for s := range configs {
			svcNames = append(svcNames, s)
		}
		sort.Strings(svcNames)
		return nil, fmt.Errorf("configured services not found: %s", strings.Join(svcNames, ", "))
	}

	return descs, nil
}

func dial(ctx context.Context, network, addr string, creds credentials.TransportCredentials, failFast bool, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	if failFast {
		return grpcurl.BlockingDial(ctx, network, addr, creds, opts...)
	}
	// BlockingDial will return the first error returned. It is meant for interactive use.
	// If we don't want to fail fast, then we need to do a more customized dial.

	// TODO: perhaps this logic should be added to the grpcurl package, like in a new
	// BlockingDialNoFailFast function?

	dialer := &errTrackingDialer{
		dialer:  &net.Dialer{},
		network: network,
	}
	var errCreds errTrackingCreds
	if creds == nil {
		opts = append(opts, grpc.WithTransportCredentials(insecurecreds.NewCredentials()))
	} else {
		errCreds = errTrackingCreds{
			TransportCredentials: creds,
		}
		opts = append(opts, grpc.WithTransportCredentials(&errCreds))
	}

	cc, err := grpc.DialContext(ctx, addr, append(opts, grpc.WithBlock(), grpc.WithContextDialer(dialer.dial))...)
	if err == nil {
		return cc, nil
	}

	// prefer last observed TLS handshake error if there is one
	if err := errCreds.err(); err != nil {
		return nil, err
	}
	// otherwise, use the error the dialer last observed
	if err := dialer.err(); err != nil {
		return nil, err
	}
	// if we have no better source of error message, use what grpc.DialContext returned
	return nil, err
}

type errTrackingCreds struct {
	credentials.TransportCredentials

	mu      sync.Mutex
	lastErr error
}

func (c *errTrackingCreds) ClientHandshake(ctx context.Context, addr string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	conn, auth, err := c.TransportCredentials.ClientHandshake(ctx, addr, rawConn)
	if err != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.lastErr = err
	}
	return conn, auth, err
}

func (c *errTrackingCreds) err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}

type errTrackingDialer struct {
	dialer  *net.Dialer
	network string

	mu      sync.Mutex
	lastErr error
}

func (c *errTrackingDialer) dial(ctx context.Context, addr string) (net.Conn, error) {
	conn, err := c.dialer.DialContext(ctx, c.network, addr)
	if err != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.lastErr = err
	}
	return conn, err
}

func (c *errTrackingDialer) err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}
