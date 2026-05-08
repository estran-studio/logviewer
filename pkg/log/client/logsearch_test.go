package client_test

import (
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
)

func TestMerging(t *testing.T) {

	searchParent := client.LogSearch{
		Refresh: client.RefreshOptions{},
		Size:    ty.OptWrap(100),
	}

	searchChild := client.LogSearch{
		Refresh: client.RefreshOptions{
			Duration: ty.OptWrap("15s"),
		},
	}

	_ = searchParent.MergeInto(&searchChild)

	str, _ := ty.ToJSONString(&searchParent)

	restoreParent := client.LogSearch{}

	_ = ty.FromJSONString(str, &restoreParent)

	assert.Equal(t, searchParent.Refresh.Duration.Value, "15s", "should be the same")
	// assert.Equal(t, searchParent)

}

func TestMergingFollow(t *testing.T) {
	searchParent := client.LogSearch{
		Follow: false,
	}

	searchChild := client.LogSearch{
		Follow: true,
	}

	_ = searchParent.MergeInto(&searchChild)

	assert.True(t, searchParent.Follow, "Follow should be true after merge")

}

func TestMergingPrinterOptions(t *testing.T) {

	searchParent := client.LogSearch{

		PrinterOptions: client.PrinterOptions{

			Template: ty.OptWrap("template1"),
		},
	}

	searchChild := client.LogSearch{

		PrinterOptions: client.PrinterOptions{

			Color: ty.OptWrap(true),
		},
	}

	_ = searchParent.MergeInto(&searchChild)

	assert.Equal(t, "template1", searchParent.PrinterOptions.Template.Value)

	assert.True(t, searchParent.PrinterOptions.Color.Value)

}

func TestClone(t *testing.T) {
	t.Run("Clone creates deep copy of LogSearch", func(t *testing.T) {
		original := &client.LogSearch{
			Follow: true,
			Size:   ty.Opt[int]{Value: 100, Set: true},
			Options: ty.MI{
				"container": "test-container",
				"service":   "test-service",
			},
			Fields: ty.MS{
				"field1": "value1",
				"field2": "value2",
			},
			FieldsCondition: ty.MS{
				"field1": "equals",
			},
			   Variables: map[string]client.VariableDefinition{
				   "var1": {Description: "test variable"},
			},
		}

		clone := original.Clone()

		// Verify all fields are copied
		assert.Equal(t, original.Follow, clone.Follow)
		assert.Equal(t, original.Size.Value, clone.Size.Value)
		assert.Equal(t, original.Options["container"], clone.Options["container"])
		assert.Equal(t, original.Fields["field1"], clone.Fields["field1"])
		assert.Equal(t, original.FieldsCondition["field1"], clone.FieldsCondition["field1"])
		assert.Equal(t, original.Variables["var1"].Description, clone.Variables["var1"].Description)

		// Verify deep copy by modifying clone and checking original is unchanged
		clone.Options["container"] = "modified-container"
		clone.Fields["field1"] = "modified-value"
		clone.FieldsCondition["field1"] = "modified-condition"
		clone.Variables["var1"] = client.VariableDefinition{Description: "modified"}

		assert.Equal(t, "test-container", original.Options["container"], "Original should be unchanged")
		assert.Equal(t, "value1", original.Fields["field1"], "Original should be unchanged")
		assert.Equal(t, "equals", original.FieldsCondition["field1"], "Original should be unchanged")
		assert.Equal(t, "test variable", original.Variables["var1"].Description, "Original should be unchanged")
	})

	t.Run("Clone handles nil LogSearch", func(t *testing.T) {
		var original *client.LogSearch
		clone := original.Clone()
		assert.Nil(t, clone)
	})

	t.Run("Clone handles empty maps", func(t *testing.T) {
		original := &client.LogSearch{
			Follow: true,
		}

		clone := original.Clone()
		assert.NotNil(t, clone)
		assert.Equal(t, original.Follow, clone.Follow)
	})

	t.Run("Clone handles Filter field", func(t *testing.T) {
		   original := &client.LogSearch{
			   Filter: &client.Filter{
				   Field: "testField",
				   Op:    "equals",
				   Value: "testValue",
			   },
		}

		clone := original.Clone()
		assert.NotNil(t, clone.Filter)
		assert.Equal(t, original.Filter.Field, clone.Filter.Field)
		assert.Equal(t, original.Filter.Op, clone.Filter.Op)
		assert.Equal(t, original.Filter.Value, clone.Filter.Value)

		// Verify it's a deep copy
		clone.Filter.Field = "modifiedField"
		assert.Equal(t, "testField", original.Filter.Field, "Original Filter should be unchanged")
	})

	t.Run("Clone deeply nested filters", func(t *testing.T) {
		   original := &client.LogSearch{
			   Follow: true,
			   Filter: &client.Filter{
				   Logic: client.LogicAnd,
				   Filters: []client.Filter{
					   {Field: "level", Op: "equals", Value: "ERROR"},
					   {
						   Logic: client.LogicOr,
						   Filters: []client.Filter{
							   {Field: "app", Op: "equals", Value: "app1"},
							   {Field: "app", Op: "equals", Value: "app2"},
						   },
					   },
				   },
			   },
		}

		clone := original.Clone()

		// Verify structure is preserved
		assert.NotNil(t, clone.Filter)
		assert.Equal(t, client.LogicAnd, clone.Filter.Logic)
		assert.Len(t, clone.Filter.Filters, 2)
		assert.Equal(t, "level", clone.Filter.Filters[0].Field)
		assert.Equal(t, client.LogicOr, clone.Filter.Filters[1].Logic)

		// Verify deep copy by modifying nested filter
		clone.Filter.Filters[1].Filters[0].Value = "modified-app"

		// Original should be unchanged
		assert.Equal(t, "app1", original.Filter.Filters[1].Filters[0].Value, "Original nested filter should be unchanged")
	})

	t.Run("Clone with complex nested structure", func(t *testing.T) {
		   original := &client.LogSearch{
			   Options: ty.MI{"container": "test"},
			   Filter: &client.Filter{
				   Logic: client.LogicAnd,
				   Filters: []client.Filter{
					   {
						   Logic: client.LogicOr,
						   Filters: []client.Filter{
							   {Field: "level", Op: "equals", Value: "ERROR"},
							   {Field: "level", Op: "equals", Value: "WARN"},
						   },
					   },
					   {
						   Logic: client.LogicNot,
						   Filters: []client.Filter{
							   {Field: "app", Op: "equals", Value: "excluded-app"},
						   },
					   },
				   },
			   },
		}

		clone := original.Clone()

		// Modify clone at multiple levels
		clone.Options["container"] = "modified"
		clone.Filter.Filters[0].Filters[0].Value = "FATAL"
		clone.Filter.Filters[1].Filters[0].Field = "service"

		// Verify original is completely unchanged
		assert.Equal(t, "test", original.Options["container"])
		assert.Equal(t, "ERROR", original.Filter.Filters[0].Filters[0].Value)
		assert.Equal(t, "app", original.Filter.Filters[1].Filters[0].Field)
	})
}
