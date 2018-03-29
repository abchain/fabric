// Copyright blackpai.com. 2018 All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// usersAddCmd represents the adduser command
var usersAddCmd = &cobra.Command{
	Use:   "add",
	Short: "add a client, peer or validator user",
	Long: `add a user, role as client, peer or validator, note param id, affiliation and password is required, role must in [1, 2, 4]
	 default 1, enum 1:client  2:peer  4: validator ex:

	membership users add --id zx --role 1 --affiliation tester --password pswofzx
	`,
	Run: func(cmd *cobra.Command, args []string) {
		// fmt.Println("adduser called")
		runUsersAddCmd()
	},
}

func init() {
	usersAddCmd.Flags().StringVarP(&Id, "id", "", "", "id of user")
	usersAddCmd.Flags().StringVarP(&Affiliation, "affiliation", "", "", "affiliation of user")
	usersAddCmd.Flags().StringVarP(&Role, "role", "1", "", "role")
	usersAddCmd.Flags().StringVarP(&Password, "password", "", "", "password")
	usersCmd.AddCommand(usersAddCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// adduserCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// adduserCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func runUsersAddCmd() {
	if err := rpcUsersAdd(Url, Id, Affiliation, Password, Role); err != nil {
		fmt.Println("")
		fmt.Println(err)
		fmt.Println("")
	} else {
		fmt.Println("")
		fmt.Println("success")
		fmt.Println("")
	}
}
