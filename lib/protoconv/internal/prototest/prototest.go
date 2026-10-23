package prototest

import (
	"context"
	"fmt"
	"slices"

	_ "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// Compile compiles every file in sources, keyed by import path, into a plugin generating each of them.
func Compile(ctx context.Context, sources map[string]string) (*protogen.Plugin, error) {
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(protocompile.CompositeResolver{
			&protocompile.SourceResolver{Accessor: protocompile.SourceAccessorFromMap(sources)},
			protocompile.ResolverFunc(func(path string) (protocompile.SearchResult, error) {
				file, err := protoregistry.GlobalFiles.FindFileByPath(path)
				if err != nil {
					return protocompile.SearchResult{}, err
				}

				return protocompile.SearchResult{Desc: file}, nil
			}),
		}),
		SourceInfoMode: protocompile.SourceInfoStandard,
	}

	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	slices.Sort(names)

	files, err := compiler.Compile(ctx, names...)
	if err != nil {
		return nil, fmt.Errorf("failed to compile %v: %w", names, err)
	}

	ordered := orderedFiles{seen: map[string]bool{}}
	for _, file := range files {
		ordered.add(file)
	}

	request, err := decodedRequest(&pluginpb.CodeGeneratorRequest{
		FileToGenerate: names,
		Parameter:      new("paths=source_relative"),
		ProtoFile:      ordered.files,
	})
	if err != nil {
		return nil, err
	}

	plugin, err := protogen.Options{}.New(request)
	if err != nil {
		return nil, fmt.Errorf("failed to build the plugin for %v: %w", names, err)
	}

	return plugin, nil
}

// orderedFiles lists files after every file they import, as protoc lists them for a plugin.
type orderedFiles struct {
	seen  map[string]bool
	files []*descriptorpb.FileDescriptorProto
}

func (o *orderedFiles) add(file protoreflect.FileDescriptor) {
	if o.seen[file.Path()] {
		return
	}
	o.seen[file.Path()] = true

	imports := file.Imports()
	for index := range imports.Len() {
		o.add(imports.Get(index).FileDescriptor)
	}

	o.files = append(o.files, protodesc.ToFileDescriptorProto(file))
}

// decodedRequest round-trips the request through the wire format, so each option arrives as the
// registered extension type it would be in a request protoc sent.
func decodedRequest(request *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorRequest, error) {
	encoded, err := proto.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to encode the plugin request: %w", err)
	}

	decoded := &pluginpb.CodeGeneratorRequest{}
	if err := proto.Unmarshal(encoded, decoded); err != nil {
		return nil, fmt.Errorf("failed to decode the plugin request: %w", err)
	}

	return decoded, nil
}
