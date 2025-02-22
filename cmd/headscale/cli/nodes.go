package cli

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	survey "github.com/AlecAivazis/survey/v2"
	"github.com/juanfont/headscale"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"tailscale.com/tailcfg"
	"tailscale.com/types/wgkey"
)

func init() {
	rootCmd.AddCommand(nodeCmd)
	listNodesCmd.Flags().StringP("namespace", "n", "", "Filter by namespace")
	nodeCmd.AddCommand(listNodesCmd)

	registerNodeCmd.Flags().StringP("namespace", "n", "", "Namespace")
	err := registerNodeCmd.MarkFlagRequired("namespace")
	if err != nil {
		log.Fatalf(err.Error())
	}
	registerNodeCmd.Flags().StringP("key", "k", "", "Key")
	err = registerNodeCmd.MarkFlagRequired("key")
	if err != nil {
		log.Fatalf(err.Error())
	}
	nodeCmd.AddCommand(registerNodeCmd)

	deleteNodeCmd.Flags().IntP("identifier", "i", 0, "Node identifier (ID)")
	err = deleteNodeCmd.MarkFlagRequired("identifier")
	if err != nil {
		log.Fatalf(err.Error())
	}
	nodeCmd.AddCommand(deleteNodeCmd)

	shareMachineCmd.Flags().StringP("namespace", "n", "", "Namespace")
	err = shareMachineCmd.MarkFlagRequired("namespace")
	if err != nil {
		log.Fatalf(err.Error())
	}
	shareMachineCmd.Flags().IntP("identifier", "i", 0, "Node identifier (ID)")
	err = shareMachineCmd.MarkFlagRequired("identifier")
	if err != nil {
		log.Fatalf(err.Error())
	}
	nodeCmd.AddCommand(shareMachineCmd)

	unshareMachineCmd.Flags().StringP("namespace", "n", "", "Namespace")
	err = unshareMachineCmd.MarkFlagRequired("namespace")
	if err != nil {
		log.Fatalf(err.Error())
	}
	unshareMachineCmd.Flags().IntP("identifier", "i", 0, "Node identifier (ID)")
	err = unshareMachineCmd.MarkFlagRequired("identifier")
	if err != nil {
		log.Fatalf(err.Error())
	}
	nodeCmd.AddCommand(unshareMachineCmd)
}

var nodeCmd = &cobra.Command{
	Use:   "nodes",
	Short: "Manage the nodes of Headscale",
}

var registerNodeCmd = &cobra.Command{
	Use:   "register",
	Short: "Registers a machine to your network",
	Run: func(cmd *cobra.Command, args []string) {
		n, err := cmd.Flags().GetString("namespace")
		if err != nil {
			log.Fatalf("Error getting namespace: %s", err)
		}
		o, _ := cmd.Flags().GetString("output")

		h, err := getHeadscaleApp()
		if err != nil {
			log.Fatalf("Error initializing: %s", err)
		}
		machineIDStr, err := cmd.Flags().GetString("key")
		if err != nil {
			log.Fatalf("Error getting machine ID: %s", err)
		}
		m, err := h.RegisterMachine(machineIDStr, n)
		if strings.HasPrefix(o, "json") {
			JsonOutput(m, err, o)
			return
		}
		if err != nil {
			fmt.Printf("Cannot register machine: %s\n", err)
			return
		}
		fmt.Printf("Machine registered\n")
	},
}

var listNodesCmd = &cobra.Command{
	Use:   "list",
	Short: "List nodes",
	Run: func(cmd *cobra.Command, args []string) {
		n, err := cmd.Flags().GetString("namespace")
		if err != nil {
			log.Fatalf("Error getting namespace: %s", err)
		}
		o, _ := cmd.Flags().GetString("output")

		h, err := getHeadscaleApp()
		if err != nil {
			log.Fatalf("Error initializing: %s", err)
		}

		var namespaces []headscale.Namespace
		var namespace *headscale.Namespace
		var sharedMachines *[]headscale.Machine
		if len(n) == 0 {
			// no namespace provided, list all
			tmp, err := h.ListNamespaces()
			if err != nil {
				log.Fatalf("Error fetching namespace: %s", err)
			}
			namespaces = *tmp
		} else {
			namespace, err = h.GetNamespace(n)
			if err != nil {
				log.Fatalf("Error fetching namespace: %s", err)
			}
			namespaces = append(namespaces, *namespace)

			sharedMachines, err = h.ListSharedMachinesInNamespace(n)
			if err != nil {
				log.Fatalf("Error fetching shared machines: %s", err)
			}
		}

		var allMachines []headscale.Machine
		for _, namespace := range namespaces {
			machines, err := h.ListMachinesInNamespace(namespace.Name)
			if err != nil {
				log.Fatalf("Error fetching machines: %s", err)
			}
			allMachines = append(allMachines, *machines...)
		}

		// listing sharedMachines is only relevant when a particular namespace is
		// requested
		if sharedMachines != nil {
			allMachines = append(allMachines, *sharedMachines...)
		}

		if strings.HasPrefix(o, "json") {
			JsonOutput(allMachines, err, o)
			return
		}

		if err != nil {
			log.Fatalf("Error getting nodes: %s", err)
		}

		d, err := nodesToPtables(namespace, allMachines)
		if err != nil {
			log.Fatalf("Error converting to table: %s", err)
		}

		err = pterm.DefaultTable.WithHasHeader().WithData(d).Render()
		if err != nil {
			log.Fatal(err)
		}
	},
}

var deleteNodeCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a node",
	Run: func(cmd *cobra.Command, args []string) {
		output, _ := cmd.Flags().GetString("output")
		h, err := getHeadscaleApp()
		if err != nil {
			log.Fatalf("Error initializing: %s", err)
		}
		id, err := cmd.Flags().GetInt("identifier")
		if err != nil {
			log.Fatalf("Error converting ID to integer: %s", err)
		}
		m, err := h.GetMachineByID(uint64(id))
		if err != nil {
			log.Fatalf("Error getting node: %s", err)
		}

		confirm := false
		force, _ := cmd.Flags().GetBool("force")
		if !force {
			prompt := &survey.Confirm{
				Message: fmt.Sprintf("Do you want to remove the node %s?", m.Name),
			}
			err = survey.AskOne(prompt, &confirm)
			if err != nil {
				return
			}
		}

		if confirm || force {
			err = h.DeleteMachine(m)
			if strings.HasPrefix(output, "json") {
				JsonOutput(map[string]string{"Result": "Node deleted"}, err, output)
				return
			}
			if err != nil {
				log.Fatalf("Error deleting node: %s", err)
			}
			fmt.Printf("Node deleted\n")
		} else {
			if strings.HasPrefix(output, "json") {
				JsonOutput(map[string]string{"Result": "Node not deleted"}, err, output)
				return
			}
			fmt.Printf("Node not deleted\n")
		}
	},
}

func sharingWorker(cmd *cobra.Command, args []string) (*headscale.Headscale, string, *headscale.Machine, *headscale.Namespace) {
	namespaceStr, err := cmd.Flags().GetString("namespace")
	if err != nil {
		log.Fatalf("Error getting namespace: %s", err)
	}

	output, _ := cmd.Flags().GetString("output")

	h, err := getHeadscaleApp()
	if err != nil {
		log.Fatalf("Error initializing: %s", err)
	}

	namespace, err := h.GetNamespace(namespaceStr)
	if err != nil {
		log.Fatalf("Error fetching namespace %s: %s", namespaceStr, err)
	}

	id, err := cmd.Flags().GetInt("identifier")
	if err != nil {
		log.Fatalf("Error converting ID to integer: %s", err)
	}
	machine, err := h.GetMachineByID(uint64(id))
	if err != nil {
		log.Fatalf("Error getting node: %s", err)
	}

	return h, output, machine, namespace
}

var shareMachineCmd = &cobra.Command{
	Use:   "share",
	Short: "Shares a node from the current namespace to the specified one",
	Run: func(cmd *cobra.Command, args []string) {
		h, output, machine, namespace := sharingWorker(cmd, args)
		err := h.AddSharedMachineToNamespace(machine, namespace)
		if strings.HasPrefix(output, "json") {
			JsonOutput(map[string]string{"Result": "Node shared"}, err, output)
			return
		}
		if err != nil {
			fmt.Printf("Error sharing node: %s\n", err)
			return
		}

		fmt.Println("Node shared!")
	},
}

var unshareMachineCmd = &cobra.Command{
	Use:   "unshare",
	Short: "Unshares a node from the specified namespace",
	Run: func(cmd *cobra.Command, args []string) {
		h, output, machine, namespace := sharingWorker(cmd, args)
		err := h.RemoveSharedMachineFromNamespace(machine, namespace)
		if strings.HasPrefix(output, "json") {
			JsonOutput(map[string]string{"Result": "Node unshared"}, err, output)
			return
		}
		if err != nil {
			fmt.Printf("Error unsharing node: %s\n", err)
			return
		}

		fmt.Println("Node unshared!")
	},
}

func nodesToPtables(currentNamespace *headscale.Namespace, machines []headscale.Machine) (pterm.TableData, error) {
	d := pterm.TableData{{"ID", "Name", "NodeKey", "Namespace", "IP address", "Ephemeral", "Last seen", "Online"}}

	for _, machine := range machines {
		var ephemeral bool
		if machine.AuthKey != nil && machine.AuthKey.Ephemeral {
			ephemeral = true
		}
		var lastSeen time.Time
		var lastSeenTime string
		if machine.LastSeen != nil {
			lastSeen = *machine.LastSeen
			lastSeenTime = lastSeen.Format("2006-01-02 15:04:05")
		}
		nKey, err := wgkey.ParseHex(machine.NodeKey)
		if err != nil {
			return nil, err
		}
		nodeKey := tailcfg.NodeKey(nKey)

		var online string
		if lastSeen.After(time.Now().Add(-5 * time.Minute)) { // TODO: Find a better way to reliably show if online
			online = pterm.LightGreen("true")
		} else {
			online = pterm.LightRed("false")
		}

		var namespace string
		if (currentNamespace == nil) || (currentNamespace.ID == machine.NamespaceID) {
			namespace = pterm.LightMagenta(machine.Namespace.Name)
		} else {
			// Shared into this namespace
			namespace = pterm.LightYellow(machine.Namespace.Name)
		}
		d = append(d, []string{strconv.FormatUint(machine.ID, 10), machine.Name, nodeKey.ShortString(), namespace, machine.IPAddress, strconv.FormatBool(ephemeral), lastSeenTime, online})
	}
	return d, nil
}
