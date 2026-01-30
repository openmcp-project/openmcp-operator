package utils_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/api/common"
)

func clusterWithPurposes(purposes ...string) *clustersv1alpha1.Cluster {
	return &clustersv1alpha1.Cluster{
		Spec: clustersv1alpha1.ClusterSpec{
			Purposes: purposes,
		},
	}
}

// To avoid having to add the testing dependencies to the api module, the tests for the selectors itself are contained here.
var _ = Describe("Cluster Selector Tests", func() {

	Context("Simple Selectors", func() {

		Context("IdentitySelector", func() {

			Context("Empty", func() {

				It("should return true for a nil selector", func() {
					var s *clustersv1alpha1.IdentitySelector
					Expect(s.Empty()).To(BeTrue())
				})

				It("should return true for a selector with a nil identities list", func() {
					s := &clustersv1alpha1.IdentitySelector{}
					Expect(s.Empty()).To(BeTrue())
				})

				It("should return false for a selector with an empty identities list", func() {
					s := &clustersv1alpha1.IdentitySelector{
						MatchIdentities: []common.ObjectReference{},
					}
					Expect(s.Empty()).To(BeFalse())
				})

				It("should return false for a selector with identities", func() {
					s := &clustersv1alpha1.IdentitySelector{
						MatchIdentities: []common.ObjectReference{
							{
								Name:      "foo",
								Namespace: "bar",
							},
						},
					}
					Expect(s.Empty()).To(BeFalse())
				})

			})

			Context("Matches", func() {

				It("should return true for an empty selector", func() {
					c := &clustersv1alpha1.Cluster{}
					var s *clustersv1alpha1.IdentitySelector
					Expect(s.Matches(c)).To(BeTrue())

					s = &clustersv1alpha1.IdentitySelector{}
					Expect(s.Matches(c)).To(BeTrue())
				})

				It("should return true for a nil object", func() {
					s := &clustersv1alpha1.IdentitySelector{
						MatchIdentities: []common.ObjectReference{
							{
								Name:      "foo",
								Namespace: "bar",
							},
						},
					}
					Expect(s.Matches(nil)).To(BeTrue())
				})

				It("should return true for a matching identity", func() {
					c := &clustersv1alpha1.Cluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
					}
					s := &clustersv1alpha1.IdentitySelector{
						MatchIdentities: []common.ObjectReference{
							{
								Name:      "foo",
								Namespace: "bar",
							},
						},
					}
					Expect(s.Matches(c)).To(BeTrue())
				})

				It("should return false for a non-matching identity", func() {
					c := &clustersv1alpha1.Cluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
					}
					s := &clustersv1alpha1.IdentitySelector{
						MatchIdentities: []common.ObjectReference{
							{
								Name:      "baz",
								Namespace: "bar",
							},
						},
					}
					Expect(s.Matches(c)).To(BeFalse())
				})

				It("should return false for an empty identities list", func() {
					c := &clustersv1alpha1.Cluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
					}
					s := &clustersv1alpha1.IdentitySelector{
						MatchIdentities: []common.ObjectReference{},
					}
					Expect(s.Matches(c)).To(BeFalse())
				})

				It("should return true if any identity matches", func() {
					c := &clustersv1alpha1.Cluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "foo",
							Namespace: "bar",
						},
					}
					s := &clustersv1alpha1.IdentitySelector{
						MatchIdentities: []common.ObjectReference{
							{
								Name:      "baz",
								Namespace: "bar",
							},
							{
								Name:      "foo",
								Namespace: "bar",
							},
						},
					}
					Expect(s.Matches(c)).To(BeTrue())
				})

			})

			// Nothing to test for Validate(), as it never returns an error.

		})

		Context("LabelSelector", func() {

			Context("Empty", func() {

				It("should return true for a nil selector", func() {
					var s *clustersv1alpha1.LabelSelector
					Expect(s.Empty()).To(BeTrue())
				})

				It("should return true for a selector with a nil LabelSelector", func() {
					s := &clustersv1alpha1.LabelSelector{}
					Expect(s.Empty()).To(BeTrue())
				})

				It("should return false for a selector with actual requirements", func() {
					s := &clustersv1alpha1.LabelSelector{
						MatchLabels: map[string]string{
							"key": "value",
						},
					}
					Expect(s.Empty()).To(BeFalse())
				})

			})

			// Nothing to test for Matches(obj), as it just wraps the labels.Selector implementation.

			Context("Validate", func() {

				It("should return nil for a nil selector", func() {
					var s *clustersv1alpha1.LabelSelector
					Expect(s.Validate()).To(Succeed())
				})

				It("should return nil for a valid selector", func() {
					s := &clustersv1alpha1.LabelSelector{
						MatchLabels: map[string]string{
							"key": "value",
						},
					}
					Expect(s.Validate()).To(Succeed())
				})

				It("should return an error for an invalid selector", func() {
					s := &clustersv1alpha1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "key",
								Operator: "InvalidOperator",
								Values:   []string{"value"},
							},
						},
					}
					Expect(s.Validate()).ToNot(Succeed())
				})

			})

		})

		Context("PurposeSelector", func() {

			Context("Empty", func() {

				It("should return true for a nil selector", func() {
					var s *clustersv1alpha1.PurposeSelector
					Expect(s.Empty()).To(BeTrue())
				})

				It("should return true for a selector with a nil MatchPurposes list", func() {
					s := &clustersv1alpha1.PurposeSelector{}
					Expect(s.Empty()).To(BeTrue())
				})

				It("should return true for a selector with an empty MatchPurposes list", func() {
					s := &clustersv1alpha1.PurposeSelector{
						MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{},
					}
					Expect(s.Empty()).To(BeTrue())
				})

				It("should return false for a selector with purpose requirements", func() {
					s := &clustersv1alpha1.PurposeSelector{
						MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{
							{
								Operator: clustersv1alpha1.PurposeSelectorOpContainsAll,
								Values:   []string{"purpose1"},
							},
						},
					}
					Expect(s.Empty()).To(BeFalse())
				})

			})

			Context("Matches", func() {

				It("should return true for an empty selector", func() {
					c := &clustersv1alpha1.Cluster{}
					var s *clustersv1alpha1.PurposeSelector
					Expect(s.Matches(c)).To(BeTrue())

					s = &clustersv1alpha1.PurposeSelector{}
					Expect(s.Matches(c)).To(BeTrue())

					s.MatchPurposes = []clustersv1alpha1.PurposeSelectorRequirement{}
					Expect(s.Matches(c)).To(BeTrue())
				})

				It("should return true for a nil object", func() {
					s := &clustersv1alpha1.PurposeSelector{
						MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{
							{
								Operator: clustersv1alpha1.PurposeSelectorOpContainsAll,
								Values:   []string{"purpose1"},
							},
						},
					}
					Expect(s.Matches(nil)).To(BeTrue())
				})

				It("should correctly handle the 'ContainsAll' operator", func() {
					s := &clustersv1alpha1.PurposeSelector{
						MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{
							{
								Operator: clustersv1alpha1.PurposeSelectorOpContainsAll,
								Values: []string{
									"purpose1",
									"purpose2",
								},
							},
						},
					}

					Expect(s.Matches(clusterWithPurposes())).To(BeFalse())
					Expect(s.Matches(clusterWithPurposes("purpose1"))).To(BeFalse())
					Expect(s.Matches(clusterWithPurposes("purpose2"))).To(BeFalse())
					Expect(s.Matches(clusterWithPurposes("purpose1", "purpose2"))).To(BeTrue())
					Expect(s.Matches(clusterWithPurposes("purpose3", "purpose2", "purpose2", "purpose1", "purpose4"))).To(BeTrue())
					Expect(s.Matches(clusterWithPurposes("purpose3", "purpose4"))).To(BeFalse())
				})

				It("should correctly handle the 'ContainsAny' operator", func() {
					s := &clustersv1alpha1.PurposeSelector{
						MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{
							{
								Operator: clustersv1alpha1.PurposeSelectorOpContainsAny,
								Values: []string{
									"purpose1",
									"purpose2",
								},
							},
						},
					}

					Expect(s.Matches(clusterWithPurposes())).To(BeFalse())
					Expect(s.Matches(clusterWithPurposes("purpose1"))).To(BeTrue())
					Expect(s.Matches(clusterWithPurposes("purpose2"))).To(BeTrue())
					Expect(s.Matches(clusterWithPurposes("purpose1", "purpose2"))).To(BeTrue())
					Expect(s.Matches(clusterWithPurposes("purpose3", "purpose2", "purpose2", "purpose1", "purpose4"))).To(BeTrue())
					Expect(s.Matches(clusterWithPurposes("purpose3", "purpose4"))).To(BeFalse())
				})

				It("should correctly handle the 'ContainsNone' operator", func() {
					s := &clustersv1alpha1.PurposeSelector{
						MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{
							{
								Operator: clustersv1alpha1.PurposeSelectorOpContainsNone,
								Values: []string{
									"purpose1",
									"purpose2",
								},
							},
						},
					}

					Expect(s.Matches(clusterWithPurposes())).To(BeTrue())
					Expect(s.Matches(clusterWithPurposes("purpose1"))).To(BeFalse())
					Expect(s.Matches(clusterWithPurposes("purpose2"))).To(BeFalse())
					Expect(s.Matches(clusterWithPurposes("purpose1", "purpose2"))).To(BeFalse())
					Expect(s.Matches(clusterWithPurposes("purpose3", "purpose2", "purpose2", "purpose1", "purpose4"))).To(BeFalse())
					Expect(s.Matches(clusterWithPurposes("purpose3", "purpose4"))).To(BeTrue())
				})

				It("should correctly handle the 'Equals' operator", func() {
					s := &clustersv1alpha1.PurposeSelector{
						MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{
							{
								Operator: clustersv1alpha1.PurposeSelectorOpEquals,
								Values: []string{
									"purpose1",
									"purpose2",
								},
							},
						},
					}

					Expect(s.Matches(clusterWithPurposes())).To(BeFalse())
					Expect(s.Matches(clusterWithPurposes("purpose1"))).To(BeFalse())
					Expect(s.Matches(clusterWithPurposes("purpose2"))).To(BeFalse())
					Expect(s.Matches(clusterWithPurposes("purpose1", "purpose2"))).To(BeTrue())
					Expect(s.Matches(clusterWithPurposes("purpose2", "purpose1"))).To(BeTrue())
					Expect(s.Matches(clusterWithPurposes("purpose3", "purpose2", "purpose2", "purpose1", "purpose4"))).To(BeFalse())
					Expect(s.Matches(clusterWithPurposes("purpose3", "purpose4"))).To(BeFalse())
				})

			})

			Context("Validate", func() {

				It("should return an error in case of an empty or unknown operator", func() {
					s := &clustersv1alpha1.PurposeSelector{
						MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{
							{
								Operator: "",
								Values: []string{
									"purpose1",
									"purpose2",
								},
							},
						},
					}
					Expect(s.Validate()).ToNot(Succeed())

					s.MatchPurposes[0].Operator = "unknown"
					Expect(s.Validate()).ToNot(Succeed())

					s.MatchPurposes = append(s.MatchPurposes, *s.MatchPurposes[0].DeepCopy())
					s.MatchPurposes[0].Operator = clustersv1alpha1.PurposeSelectorOpContainsAll
					Expect(s.Validate()).ToNot(Succeed())
				})

				It("should not return an error for all known operators", func() {
					s := &clustersv1alpha1.PurposeSelector{
						MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{},
					}
					for _, op := range []clustersv1alpha1.PurposeSelectorOperator{
						clustersv1alpha1.PurposeSelectorOpContainsAll,
						clustersv1alpha1.PurposeSelectorOpContainsAny,
						clustersv1alpha1.PurposeSelectorOpContainsNone,
						clustersv1alpha1.PurposeSelectorOpEquals,
					} {
						s.MatchPurposes = append(s.MatchPurposes, clustersv1alpha1.PurposeSelectorRequirement{
							Operator: op,
							Values: []string{
								"purpose1",
								"purpose2",
							},
						})
					}
					Expect(s.Validate()).To(Succeed())
				})

			})

		})

	})

	Context("Aggregate Selectors", func() {

		Context("IdentityLabelPurposeSelector", func() {

			exampleSelector := func() *clustersv1alpha1.IdentityLabelPurposeSelector {
				return &clustersv1alpha1.IdentityLabelPurposeSelector{
					IdentitySelector: clustersv1alpha1.IdentitySelector{
						MatchIdentities: []common.ObjectReference{
							{
								Name:      "foo",
								Namespace: "bar",
							},
						},
					},
					LabelSelector: clustersv1alpha1.LabelSelector{
						MatchLabels: map[string]string{
							"foo": "bar",
						},
					},
					PurposeSelector: clustersv1alpha1.PurposeSelector{
						MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{
							{
								Operator: clustersv1alpha1.PurposeSelectorOpContainsAny,
								Values: []string{
									"purpose1",
									"purpose2",
								},
							},
						},
					},
				}
			}

			Context("Empty", func() {

				It("should return true for a nil or empty selector", func() {
					var s *clustersv1alpha1.IdentityLabelPurposeSelector
					Expect(s.Empty()).To(BeTrue())

					s = &clustersv1alpha1.IdentityLabelPurposeSelector{}
					Expect(s.Empty()).To(BeTrue())
				})

				It("should return false if any of the nested selectors is actually set", func() {
					s := &clustersv1alpha1.IdentityLabelPurposeSelector{
						IdentitySelector: clustersv1alpha1.IdentitySelector{
							MatchIdentities: []common.ObjectReference{
								{
									Name:      "foo",
									Namespace: "bar",
								},
							},
						},
					}
					Expect(s.Empty()).To(BeFalse())

					s = &clustersv1alpha1.IdentityLabelPurposeSelector{
						LabelSelector: clustersv1alpha1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
					}
					Expect(s.Empty()).To(BeFalse())

					s = &clustersv1alpha1.IdentityLabelPurposeSelector{
						PurposeSelector: clustersv1alpha1.PurposeSelector{
							MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{
								{
									Operator: clustersv1alpha1.PurposeSelectorOpContainsAll,
									Values: []string{
										"purpose1",
										"purpose2",
									},
								},
							},
						},
					}
					Expect(s.Empty()).To(BeFalse())
				})

			})

			Context("Matches", func() {

				It("should return true for an empty selector or a nil object", func() {
					var s *clustersv1alpha1.IdentityLabelPurposeSelector
					Expect(s.Matches(clusterWithPurposes())).To(BeTrue())

					s = &clustersv1alpha1.IdentityLabelPurposeSelector{
						IdentitySelector: clustersv1alpha1.IdentitySelector{
							MatchIdentities: []common.ObjectReference{
								{
									Name:      "foo",
									Namespace: "bar",
								},
							},
						},
					}
					Expect(s.Matches(nil)).To(BeTrue())
				})

				It("should only take the identity selector into account if set", func() {
					s := exampleSelector()

					c1 := clusterWithPurposes()
					c1.SetName("foo")
					c1.SetNamespace("bar")
					Expect(s.Matches(c1)).To(BeTrue())

					c2 := clusterWithPurposes("purpose1")
					c2.SetLabels(map[string]string{
						"foo": "bar",
					})
					Expect(s.Matches(c2)).To(BeFalse())
				})

				It("should evaluate all other selectors if the identity one is not set", func() {
					s := exampleSelector()
					s.MatchIdentities = nil

					// missing labels and purposes
					c1 := clusterWithPurposes()
					Expect(s.Matches(c1)).To(BeFalse())

					// missing labels
					c2 := clusterWithPurposes("purpose1")
					Expect(s.Matches(c2)).To(BeFalse())

					// missing purposes
					c3 := clusterWithPurposes()
					c3.SetLabels(map[string]string{
						"foo": "bar",
					})
					Expect(s.Matches(c3)).To(BeFalse())

					// match
					c4 := clusterWithPurposes("purpose2")
					c4.SetLabels(map[string]string{
						"foo": "bar",
					})
					Expect(s.Matches(c4)).To(BeTrue())
				})

			})

			Context("Validate", func() {

				It("should return an error if any of the nested selectors is invalid", func() {
					s1 := &clustersv1alpha1.IdentityLabelPurposeSelector{
						LabelSelector: clustersv1alpha1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "asdf",
									Operator: "unknown",
								},
							},
						},
					}
					Expect(s1.Validate()).ToNot(Succeed())

					s2 := &clustersv1alpha1.IdentityLabelPurposeSelector{
						PurposeSelector: clustersv1alpha1.PurposeSelector{
							MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{
								{
									Operator: "unknown",
								},
							},
						},
					}
					Expect(s2.Validate()).ToNot(Succeed())
				})

				It("should not return an error if all nested selectors are valid", func() {
					Expect(exampleSelector().Validate()).To(Succeed())
				})

			})

		})

		Context("IdentityLabelSelector", func() {

			exampleSelector := func() *clustersv1alpha1.IdentityLabelSelector {
				return &clustersv1alpha1.IdentityLabelSelector{
					IdentitySelector: clustersv1alpha1.IdentitySelector{
						MatchIdentities: []common.ObjectReference{
							{
								Name:      "foo",
								Namespace: "bar",
							},
						},
					},
					LabelSelector: clustersv1alpha1.LabelSelector{
						MatchLabels: map[string]string{
							"foo": "bar",
						},
					},
				}
			}

			Context("Empty", func() {

				It("should return true for a nil or empty selector", func() {
					var s *clustersv1alpha1.IdentityLabelSelector
					Expect(s.Empty()).To(BeTrue())

					s = &clustersv1alpha1.IdentityLabelSelector{}
					Expect(s.Empty()).To(BeTrue())
				})

				It("should return false if any of the nested selectors is actually set", func() {
					s := &clustersv1alpha1.IdentityLabelSelector{
						IdentitySelector: clustersv1alpha1.IdentitySelector{
							MatchIdentities: []common.ObjectReference{
								{
									Name:      "foo",
									Namespace: "bar",
								},
							},
						},
					}
					Expect(s.Empty()).To(BeFalse())

					s = &clustersv1alpha1.IdentityLabelSelector{
						LabelSelector: clustersv1alpha1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
					}
					Expect(s.Empty()).To(BeFalse())
				})

			})

			Context("Matches", func() {

				It("should return true for an empty selector or a nil object", func() {
					var s *clustersv1alpha1.IdentityLabelSelector
					Expect(s.Matches(clusterWithPurposes())).To(BeTrue())

					s = &clustersv1alpha1.IdentityLabelSelector{
						IdentitySelector: clustersv1alpha1.IdentitySelector{
							MatchIdentities: []common.ObjectReference{
								{
									Name:      "foo",
									Namespace: "bar",
								},
							},
						},
					}
					Expect(s.Matches(nil)).To(BeTrue())
				})

				It("should only take the identity selector into account if set", func() {
					s := exampleSelector()

					c1 := clusterWithPurposes()
					c1.SetName("foo")
					c1.SetNamespace("bar")
					Expect(s.Matches(c1)).To(BeTrue())

					c2 := clusterWithPurposes()
					c2.SetLabels(map[string]string{
						"foo": "bar",
					})
					Expect(s.Matches(c2)).To(BeFalse())
				})

				It("should evaluate all other selectors if the identity one is not set", func() {
					s := exampleSelector()
					s.MatchIdentities = nil

					// missing labels
					c1 := clusterWithPurposes()
					Expect(s.Matches(c1)).To(BeFalse())

					// match
					c2 := clusterWithPurposes()
					c2.SetLabels(map[string]string{
						"foo": "bar",
					})
					Expect(s.Matches(c2)).To(BeTrue())
				})

			})

			Context("Validate", func() {

				It("should return an error if any of the nested selectors is invalid", func() {
					s1 := &clustersv1alpha1.IdentityLabelSelector{
						LabelSelector: clustersv1alpha1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "asdf",
									Operator: "unknown",
								},
							},
						},
					}
					Expect(s1.Validate()).ToNot(Succeed())
				})

				It("should not return an error if all nested selectors are valid", func() {
					Expect(exampleSelector().Validate()).To(Succeed())
				})

			})

		})

		Context("IdentityPurposeSelector", func() {

			exampleSelector := func() *clustersv1alpha1.IdentityPurposeSelector {
				return &clustersv1alpha1.IdentityPurposeSelector{
					IdentitySelector: clustersv1alpha1.IdentitySelector{
						MatchIdentities: []common.ObjectReference{
							{
								Name:      "foo",
								Namespace: "bar",
							},
						},
					},
					PurposeSelector: clustersv1alpha1.PurposeSelector{
						MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{
							{
								Operator: clustersv1alpha1.PurposeSelectorOpContainsAny,
								Values: []string{
									"purpose1",
									"purpose2",
								},
							},
						},
					},
				}
			}

			Context("Empty", func() {

				It("should return true for a nil or empty selector", func() {
					var s *clustersv1alpha1.IdentityPurposeSelector
					Expect(s.Empty()).To(BeTrue())

					s = &clustersv1alpha1.IdentityPurposeSelector{}
					Expect(s.Empty()).To(BeTrue())
				})

				It("should return false if any of the nested selectors is actually set", func() {
					s := &clustersv1alpha1.IdentityPurposeSelector{
						IdentitySelector: clustersv1alpha1.IdentitySelector{
							MatchIdentities: []common.ObjectReference{
								{
									Name:      "foo",
									Namespace: "bar",
								},
							},
						},
					}
					Expect(s.Empty()).To(BeFalse())

					s = &clustersv1alpha1.IdentityPurposeSelector{
						PurposeSelector: clustersv1alpha1.PurposeSelector{
							MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{
								{
									Operator: clustersv1alpha1.PurposeSelectorOpContainsAll,
									Values: []string{
										"purpose1",
										"purpose2",
									},
								},
							},
						},
					}
					Expect(s.Empty()).To(BeFalse())
				})

			})

			Context("Matches", func() {

				It("should return true for an empty selector or a nil object", func() {
					var s *clustersv1alpha1.IdentityPurposeSelector
					Expect(s.Matches(clusterWithPurposes())).To(BeTrue())

					s = &clustersv1alpha1.IdentityPurposeSelector{
						IdentitySelector: clustersv1alpha1.IdentitySelector{
							MatchIdentities: []common.ObjectReference{
								{
									Name:      "foo",
									Namespace: "bar",
								},
							},
						},
					}
					Expect(s.Matches(nil)).To(BeTrue())
				})

				It("should only take the identity selector into account if set", func() {
					s := exampleSelector()

					c1 := clusterWithPurposes()
					c1.SetName("foo")
					c1.SetNamespace("bar")
					Expect(s.Matches(c1)).To(BeTrue())

					c2 := clusterWithPurposes("purpose1")
					c2.SetLabels(map[string]string{
						"foo": "bar",
					})
					Expect(s.Matches(c2)).To(BeFalse())
				})

				It("should evaluate all other selectors if the identity one is not set", func() {
					s := exampleSelector()
					s.MatchIdentities = nil

					// missing purposes
					c1 := clusterWithPurposes()
					Expect(s.Matches(c1)).To(BeFalse())

					// match
					c2 := clusterWithPurposes("purpose2")
					Expect(s.Matches(c2)).To(BeTrue())
				})

			})

			Context("Validate", func() {

				It("should return an error if any of the nested selectors is invalid", func() {
					s1 := &clustersv1alpha1.IdentityPurposeSelector{
						PurposeSelector: clustersv1alpha1.PurposeSelector{
							MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{
								{
									Operator: "unknown",
								},
							},
						},
					}
					Expect(s1.Validate()).ToNot(Succeed())
				})

				It("should not return an error if all nested selectors are valid", func() {
					Expect(exampleSelector().Validate()).To(Succeed())
				})

			})

		})

		Context("LabelPurposeSelector", func() {

			exampleSelector := func() *clustersv1alpha1.LabelPurposeSelector {
				return &clustersv1alpha1.LabelPurposeSelector{
					LabelSelector: clustersv1alpha1.LabelSelector{
						MatchLabels: map[string]string{
							"foo": "bar",
						},
					},
					PurposeSelector: clustersv1alpha1.PurposeSelector{
						MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{
							{
								Operator: clustersv1alpha1.PurposeSelectorOpContainsAny,
								Values: []string{
									"purpose1",
									"purpose2",
								},
							},
						},
					},
				}
			}

			Context("Empty", func() {

				It("should return true for a nil or empty selector", func() {
					var s *clustersv1alpha1.LabelPurposeSelector
					Expect(s.Empty()).To(BeTrue())

					s = &clustersv1alpha1.LabelPurposeSelector{}
					Expect(s.Empty()).To(BeTrue())
				})

				It("should return false if any of the nested selectors is actually set", func() {
					s := &clustersv1alpha1.LabelPurposeSelector{
						LabelSelector: clustersv1alpha1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
					}
					Expect(s.Empty()).To(BeFalse())

					s = &clustersv1alpha1.LabelPurposeSelector{
						PurposeSelector: clustersv1alpha1.PurposeSelector{
							MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{
								{
									Operator: clustersv1alpha1.PurposeSelectorOpContainsAll,
									Values: []string{
										"purpose1",
										"purpose2",
									},
								},
							},
						},
					}
					Expect(s.Empty()).To(BeFalse())
				})

			})

			Context("Matches", func() {

				It("should return true for an empty selector or a nil object", func() {
					var s *clustersv1alpha1.LabelPurposeSelector
					Expect(s.Matches(clusterWithPurposes())).To(BeTrue())

					s = &clustersv1alpha1.LabelPurposeSelector{
						LabelSelector: clustersv1alpha1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
					}
					Expect(s.Matches(nil)).To(BeTrue())
				})

				It("should evaluate all selectors", func() {
					s := exampleSelector()

					// missing labels and purposes
					c1 := clusterWithPurposes()
					Expect(s.Matches(c1)).To(BeFalse())

					// missing labels
					c2 := clusterWithPurposes("purpose1")
					Expect(s.Matches(c2)).To(BeFalse())

					// missing purposes
					c3 := clusterWithPurposes()
					c3.SetLabels(map[string]string{
						"foo": "bar",
					})
					Expect(s.Matches(c3)).To(BeFalse())

					// match
					c4 := clusterWithPurposes("purpose2")
					c4.SetLabels(map[string]string{
						"foo": "bar",
					})
					Expect(s.Matches(c4)).To(BeTrue())
				})

			})

			Context("Validate", func() {

				It("should return an error if any of the nested selectors is invalid", func() {
					s1 := &clustersv1alpha1.LabelPurposeSelector{
						LabelSelector: clustersv1alpha1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "asdf",
									Operator: "unknown",
								},
							},
						},
					}
					Expect(s1.Validate()).ToNot(Succeed())

					s2 := &clustersv1alpha1.LabelPurposeSelector{
						PurposeSelector: clustersv1alpha1.PurposeSelector{
							MatchPurposes: []clustersv1alpha1.PurposeSelectorRequirement{
								{
									Operator: "unknown",
								},
							},
						},
					}
					Expect(s2.Validate()).ToNot(Succeed())
				})

				It("should not return an error if all nested selectors are valid", func() {
					Expect(exampleSelector().Validate()).To(Succeed())
				})

			})

		})

	})

})

var _ = Describe("Cluster Selector Predicate Tests", func() {

})
