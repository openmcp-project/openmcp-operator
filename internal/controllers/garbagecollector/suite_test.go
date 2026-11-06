package garbagecollector_test

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/openmcp-project/controller-utils/pkg/periodic"

	"github.com/openmcp-project/openmcp-operator/internal/config"
)

func TestComponentUtils(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "GarbageCollector Test Suite")
}

// testPeriodic is a minimal implementation of periodic.Periodic for use in tests.
// Only Log() and SetInterval() are used by the run logic; all other methods panic.
type testPeriodic struct {
	interval time.Duration
}

var _ periodic.Periodic[*config.GarbageCollectorConfig] = &testPeriodic{}

func (p *testPeriodic) Log() logging.Logger           { return logging.Discard() }
func (p *testPeriodic) SetInterval(d time.Duration)   { p.interval = d }
func (p *testPeriodic) Name() string                  { return "dummy" }
func (p *testPeriodic) Enable()                       { panic("not implemented") }
func (p *testPeriodic) Disable()                      { panic("not implemented") }
func (p *testPeriodic) IsDisabled() bool              { panic("not implemented") }
func (p *testPeriodic) GetInterval() time.Duration    { return p.interval }
func (p *testPeriodic) Trigger(_ bool)                { panic("not implemented") }
func (p *testPeriodic) GetTrigger() chan<- bool       { panic("not implemented") }
func (p *testPeriodic) LastRunTime() time.Time        { panic("not implemented") }
func (p *testPeriodic) NextRunTime() time.Time        { panic("not implemented") }
func (p *testPeriodic) Start(_ context.Context) error { panic("not implemented") }
