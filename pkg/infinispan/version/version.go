package version

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/blang/semver"
	"github.com/go-logr/logr"
)

type unknownError struct {
	version *semver.Version
}

func (e *unknownError) Error() string {
	return fmt.Sprintf("unknown version: %v", e.version)
}

type Operand struct {
	UpstreamVersion   *semver.Version `json:"upstream-version,omitempty"`
	DownstreamVersion *semver.Version `json:"downstream-version,omitempty"`
	Image             string          `json:"image"`
	CVE               bool            `json:"cve"`
	Deprecated        bool            `json:"deprecated"`
}

// Ref returns the string used to represent this Operand in the Infinispan CR spec and status
func (o Operand) Ref() string {
	if o.DownstreamVersion != nil {
		return o.DownstreamVersion.String()
	}
	return o.UpstreamVersion.String()
}

func (o Operand) String() string {
	return fmt.Sprintf("Upstream=%v, Downstream=%v, Image=%s, CVE=%t, Deprecated=%t", o.UpstreamVersion, o.DownstreamVersion, o.Image, o.CVE, o.Deprecated)
}

func (o Operand) Validate() error {
	if o.UpstreamVersion == nil {
		return fmt.Errorf("upstream-version field must be specified")
	}
	if o.Image == "" {
		return fmt.Errorf("image field must be specified")
	}
	return nil
}

func (o Operand) EQ(other Operand) bool {
	if o.DownstreamVersion != nil {
		return o.DownstreamVersion.EQ(*other.DownstreamVersion)
	}
	return o.UpstreamVersion.EQ(*other.UpstreamVersion)
}

func (o Operand) LT(other Operand) bool {
	if o.DownstreamVersion != nil {
		return o.DownstreamVersion.LT(*other.DownstreamVersion)
	}
	return o.UpstreamVersion.LT(*other.UpstreamVersion)
}

func UnknownError(v *semver.Version) error {
	return &unknownError{v}
}

type Manager struct {
	operandMap map[string]*Operand
	Operands   []*Operand
}

func (m *Manager) WithRef(version string) (Operand, error) {
	operand, exists := m.operandMap[version]
	if !exists {
		v, err := semver.Parse(version)
		if err != nil {
			return Operand{}, err
		}
		return Operand{}, UnknownError(&v)
	}
	return *operand, nil
}

// Latest returns the most recent Operand release
func (m *Manager) Latest() Operand {
	return *m.Operands[len(m.Operands)-1]
}

func (m *Manager) Log(log logr.Logger) {
	if bytes, err := json.MarshalIndent(m.Operands, "", "  "); err != nil {
		log.Error(err, "unable to log VersionManager content")
	} else {
		log.Info(fmt.Sprintf("Loaded Operand Versions:\n%s", bytes))
	}
}

func ManagerFromEnv(envName string) (*Manager, error) {
	env := os.Getenv(envName)
	if env == "" {
		return nil, fmt.Errorf("unable to create version Manager, env '%s' is empty", envName)
	}
	return ManagerFromJson(env)
}

func ManagerFromJson(jsonStr string) (*Manager, error) {
	operands := make([]*Operand, 0)
	if err := json.Unmarshal([]byte(jsonStr), &operands); err != nil {
		fmt.Println(err)
		return nil, err
	}

	numOperands := len(operands)
	manager := &Manager{
		operandMap: make(map[string]*Operand, numOperands),
		Operands:   make([]*Operand, numOperands),
	}
	for i, o := range operands {
		if err := o.Validate(); err != nil {
			return nil, err
		}

		key := o.Ref()
		if _, exists := manager.operandMap[key]; exists {
			return nil, fmt.Errorf("multiple operands have the same version reference '%s'", key)
		}
		manager.operandMap[key] = o
		manager.Operands[i] = o
	}
	sort.Slice(manager.Operands, func(i, j int) bool {
		return manager.Operands[i].LT(*manager.Operands[j])
	})
	return manager, nil
}
