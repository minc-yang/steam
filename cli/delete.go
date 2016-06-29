package cli

import "github.com/spf13/cobra"

var deleteHelp = `
delete [resource-type]
Deletes the specified resource from the database.
Examples:

    $ steam delete cluster
`

func delete_(c *context) *cobra.Command {
	cmd := newCmd(c, deleteHelp, nil)
	cmd.AddCommand(deleteCluster(c))
	cmd.AddCommand(deleteEngine(c))
	cmd.AddCommand(deleteModel(c))
	cmd.AddCommand(deleteService(c))
	cmd.AddCommand(deleteRole(c))
	cmd.AddCommand(deleteWorkgroup(c))
	return cmd
}
