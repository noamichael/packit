package syft2

import (
	"fmt"
	"sort"
	"strconv"

	stereoscopeFile "github.com/anchore/stereoscope/pkg/file"
	"github.com/anchore/syft/syft/artifact"
	"github.com/anchore/syft/syft/cpe"
	"github.com/anchore/syft/syft/file"
	"github.com/anchore/syft/syft/formats/syftjson/model"
	"github.com/anchore/syft/syft/linux"
	"github.com/anchore/syft/syft/pkg"
	"github.com/anchore/syft/syft/sbom"
	"github.com/anchore/syft/syft/source"
	internalmodel "github.com/paketo-buildpacks/packit/v2/sbom/internal/formats/syft2/model"
	syft2source "github.com/paketo-buildpacks/packit/v2/sbom/internal/formats/syft2/source"
)

// NOTE: Adaptions have been added to functions in this file to translate from latest
// syft package representations to legacy JSON schema

func ToFormatModel(s sbom.SBOM) internalmodel.Document {
	src, err := toSourceModel(s.Source)
	if err != nil { //nolint:staticcheck
		// log.Warnf("unable to create syft-json source object: %+v", err)
	}

	return internalmodel.Document{
		Artifacts:             toPackageModels(s.Artifacts.PackageCatalog),
		ArtifactRelationships: toRelationshipModel(s.Relationships),
		Files:                 toFile(s),
		Secrets:               toSecrets(s.Artifacts.Secrets),
		Source:                src,
		Distro:                toDistroModel(s.Artifacts.LinuxDistribution),
		Descriptor:            toDescriptor(s.Descriptor),
		Schema: model.Schema{
			Version: JSONSchemaVersion,
			URL:     fmt.Sprintf("https://raw.githubusercontent.com/anchore/syft/main/schema/json/schema-%s.json", JSONSchemaVersion),
		},
	}
}

func toDescriptor(d sbom.Descriptor) model.Descriptor {
	return model.Descriptor{
		Name:          d.Name,
		Version:       d.Version,
		Configuration: d.Configuration,
	}
}

func toSecrets(data map[source.Coordinates][]file.SearchResult) []model.Secrets {
	results := make([]model.Secrets, 0)
	for coordinates, secrets := range data {
		results = append(results, model.Secrets{
			Location: coordinates,
			Secrets:  secrets,
		})
	}

	// sort by real path then virtual path to ensure the result is stable across multiple runs
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Location.RealPath < results[j].Location.RealPath
	})
	return results
}

func toFile(s sbom.SBOM) []model.File {
	results := make([]model.File, 0)
	artifacts := s.Artifacts

	for _, coordinates := range s.AllCoordinates() {
		var metadata *source.FileMetadata
		if metadataForLocation, exists := artifacts.FileMetadata[coordinates]; exists {
			metadata = &metadataForLocation
		}

		var digests []file.Digest
		if digestsForLocation, exists := artifacts.FileDigests[coordinates]; exists {
			digests = digestsForLocation
		}

		var contents string
		if contentsForLocation, exists := artifacts.FileContents[coordinates]; exists {
			contents = contentsForLocation
		}

		results = append(results, model.File{
			ID:       string(coordinates.ID()),
			Location: coordinates,
			Metadata: toFileMetadataEntry(coordinates, metadata),
			Digests:  digests,
			Contents: contents,
		})
	}

	// sort by real path then virtual path to ensure the result is stable across multiple runs
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Location.RealPath < results[j].Location.RealPath
	})
	return results
}

func toFileMetadataEntry(coordinates source.Coordinates, metadata *source.FileMetadata) *model.FileMetadataEntry {
	if metadata == nil {
		return nil
	}

	mode, err := strconv.Atoi(fmt.Sprintf("%o", metadata.Mode))
	if err != nil {
		// log.Warnf("invalid mode found in file catalog @ location=%+v mode=%q: %+v", coordinates, metadata.Mode, err
		mode = 0
	}

	return &model.FileMetadataEntry{
		Mode:            mode,
		Type:            toFileType(metadata.Type),
		LinkDestination: metadata.LinkDestination,
		UserID:          metadata.UserID,
		GroupID:         metadata.GroupID,
		MIMEType:        metadata.MIMEType,
	}
}

func toFileType(ty stereoscopeFile.Type) string {
	switch ty {
	case stereoscopeFile.TypeSymLink:
		return "SymbolicLink"
	case stereoscopeFile.TypeHardLink:
		return "HardLink"
	case stereoscopeFile.TypeDirectory:
		return "Directory"
	case stereoscopeFile.TypeSocket:
		return "Socket"
	case stereoscopeFile.TypeBlockDevice:
		return "BlockDevice"
	case stereoscopeFile.TypeCharacterDevice:
		return "CharacterDevice"
	case stereoscopeFile.TypeFIFO:
		return "FIFONode"
	case stereoscopeFile.TypeRegular:
		return "RegularFile"
	case stereoscopeFile.TypeIrregular:
		return "IrregularFile"
	default:
		return "Unknown"
	}
}

func toPackageModels(catalog *pkg.Catalog) []internalmodel.Package {
	artifacts := make([]internalmodel.Package, 0)
	if catalog == nil {
		return artifacts
	}
	for _, p := range catalog.Sorted() {
		artifacts = append(artifacts, toPackageModel(p))
	}
	return artifacts
}

// toPackageModel crates a new Package from the given pkg.Package.
func toPackageModel(p pkg.Package) internalmodel.Package {
	var cpes = make([]string, len(p.CPEs))
	for i, c := range p.CPEs {
		cpes[i] = cpe.String(c)
	}

	var licenses = make([]string, 0)
	if p.Licenses != nil {
		licenses = p.Licenses
	}

	locations := p.Locations.ToSlice()
	var coordinates = make([]source.Coordinates, len(locations))
	for i, l := range locations {
		coordinates[i] = l.Coordinates
	}

	return internalmodel.Package{
		PackageBasicData: internalmodel.PackageBasicData{
			ID:        string(p.ID()),
			Name:      p.Name,
			Version:   p.Version,
			Type:      p.Type,
			FoundBy:   p.FoundBy,
			Locations: coordinates,
			Licenses:  licenses,
			Language:  p.Language,
			CPEs:      cpes,
			PURL:      p.PURL,
		},
		PackageCustomData: internalmodel.PackageCustomData{
			MetadataType: p.MetadataType,
			Metadata:     p.Metadata,
		},
	}
}

func toRelationshipModel(relationships []artifact.Relationship) []model.Relationship {
	result := make([]model.Relationship, len(relationships))
	for i, r := range relationships {
		result[i] = model.Relationship{
			Parent:   string(r.From.ID()),
			Child:    string(r.To.ID()),
			Type:     string(r.Type),
			Metadata: r.Data,
		}
	}
	return result
}

// toSourceModel creates a new source object to be represented into JSON.
// NOTE: THIS FUNCTION is NOT identical to the one that appears in the original version of this file.
// It converts ImageMetadata into a struct that matches the old Syft schema.
func toSourceModel(src source.Metadata) (internalmodel.Source, error) {
	switch src.Scheme {
	case source.ImageScheme:
		return internalmodel.Source{
			Type: "image",
			// convert src.ImageMetadata into a struct with the old syft metadata fields
			Target: syft2source.ConvertImageMetadata(src.ImageMetadata),
		}, nil
	case source.DirectoryScheme:
		return internalmodel.Source{
			Type:   "directory",
			Target: src.Path,
		}, nil
	case source.FileScheme:
		return internalmodel.Source{
			Type:   "file",
			Target: src.Path,
		}, nil
	default:
		return internalmodel.Source{}, fmt.Errorf("unsupported source: %q", src.Scheme)
	}
}

// // toDistroModel creates a struct with the Linux distribution to be represented in JSON.
// NOTE: THIS FUNCTION is NOT identical to the one that appears in the original version of this file.
// It now converts from a linux.Release to a model.Distro to maintain backward compatibility.
func toDistroModel(d *linux.Release) internalmodel.Distro {
	if d == nil {
		return internalmodel.Distro{}
	}

	idLike := d.ID
	if len(d.IDLike) > 0 {
		// TODO: (packit) Is picking the 1st from this list the right thing to do?
		idLike = d.IDLike[0]
	}

	return internalmodel.Distro{
		Name:    d.ID,
		Version: d.Version,
		IDLike:  idLike,
	}
}
