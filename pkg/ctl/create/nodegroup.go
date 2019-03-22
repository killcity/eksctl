package create

import (
	"fmt"
	"os"
	"strings"

	"github.com/gobwas/glob"
	"github.com/kris-nova/logger"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	api "github.com/weaveworks/eksctl/pkg/apis/eksctl.io/v1alpha4"
	"github.com/weaveworks/eksctl/pkg/authconfigmap"
	"github.com/weaveworks/eksctl/pkg/ctl/cmdutils"
	"github.com/weaveworks/eksctl/pkg/eks"
	"github.com/weaveworks/eksctl/pkg/printers"
	"github.com/weaveworks/eksctl/pkg/utils"
)

var (
	updateAuthConfigMap bool
	nodeGroupFilter     = ""
)

func createNodeGroupCmd(g *cmdutils.Grouping) *cobra.Command {
	p := &api.ProviderConfig{}
	cfg := api.NewClusterConfig()
	ng := cfg.NewNodeGroup()

	cfg.Metadata.Version = "auto"

	cmd := &cobra.Command{
		Use:     "nodegroup",
		Short:   "Create a nodegroup",
		Aliases: []string{"ng"},
		Run: func(cmd *cobra.Command, args []string) {
			if err := doCreateNodeGroups(p, cfg, cmdutils.GetNameArg(args), cmd); err != nil {
				logger.Critical("%s\n", err.Error())
				os.Exit(1)
			}
		},
	}

	group := g.New(cmd)

	exampleNodeGroupName := utils.NodeGroupName("", "")

	group.InFlagSet("General", func(fs *pflag.FlagSet) {
		fs.StringVar(&cfg.Metadata.Name, "cluster", "", "name of the EKS cluster to add the nodegroup to")
		cmdutils.AddRegionFlag(fs, p)
		cmdutils.AddVersionFlag(fs, cfg.Metadata, `for nodegroups "auto" and "latest" can be used to automatically inherit version from the control plane or force latest`)
		fs.StringVarP(&clusterConfigFile, "config-file", "f", "", "load configuration from a file")
		fs.StringVarP(&nodeGroupFilter, "only", "", "",
			"select a subset of nodegroups via comma-separted list of globs, e.g.: 'ng-*,nodegroup?,N*group'")
		cmdutils.AddUpdateAuthConfigMap(&updateAuthConfigMap, fs, "Remove nodegroup IAM role from aws-auth configmap")
	})

	group.InFlagSet("New nodegroup", func(fs *pflag.FlagSet) {
		fs.StringVarP(&ng.Name, "name", "n", "", fmt.Sprintf("name of the new nodegroup (generated if unspecified, e.g. %q)", exampleNodeGroupName))
		cmdutils.AddCommonCreateNodeGroupFlags(cmd, fs, p, cfg, ng)
	})

	group.InFlagSet("IAM addons", func(fs *pflag.FlagSet) {
		cmdutils.AddCommonCreateNodeGroupIAMAddonsFlags(fs, ng)
	})

	cmdutils.AddCommonFlagsForAWS(group, p, true)

	group.AddTo(cmd)

	return cmd
}

func filterNodeGroups(cfg *api.ClusterConfig) error {
	if nodeGroupFilter == "" {
		// no filter supplied
		return nil
	}
	globstrs := strings.Split(nodeGroupFilter, ",")
	globs := make([]glob.Glob, len(globstrs))
	for idx, g := range globstrs {
		globs[idx] = glob.MustCompile(g)
	}
	nodegroups := cfg.NodeGroups
	filtered := make([]*api.NodeGroup, 0)
	for _, ng := range nodegroups {
		for _, g := range globs {
			if g.Match(ng.Name) {
				filtered = append(filtered, ng)
				break
			}
		}
	}
	if len(filtered) == 0 {
		return fmt.Errorf("No nodegroups match filter specification: %s", nodeGroupFilter)
	}
	cfg.NodeGroups = filtered
	return nil
}

func checkVersion(ctl *eks.ClusterProvider, meta *api.ClusterMeta) error {
	switch meta.Version {
	case "auto":
		break
	case "":
		meta.Version = "auto"
	case "latest":
		meta.Version = api.LatestVersion
		logger.Info("will use version latest version (%s) for new nodegroup(s)", meta.Version)
	default:
		validVersion := false
		for _, v := range api.SupportedVersions() {
			if meta.Version == v {
				validVersion = true
			}
		}
		if !validVersion {
			return fmt.Errorf("invalid version %s, supported values: auto, latest, %s", meta.Version, strings.Join(api.SupportedVersions(), ", "))
		}
	}

	if v := ctl.ControlPlaneVersion(); v == "" {
		return fmt.Errorf("unable to get control plane version")
	} else if meta.Version == "auto" {
		meta.Version = v
		logger.Info("will use version %s for new nodegroup(s) based on control plane version", meta.Version)
	} else if meta.Version != v {
		hint := "--version=auto"
		if clusterConfigFile != "" {
			hint = "metadata.version: auto"
		}
		logger.Warning("will use version %s for new nodegroup(s), while control plane version is %s; to automatically inherit the version use %q", meta.Version, v, hint)
	}

	return nil
}

func doCreateNodeGroups(p *api.ProviderConfig, cfg *api.ClusterConfig, nameArg string, cmd *cobra.Command) error {
	meta := cfg.Metadata

	printer := printers.NewJSONPrinter()

	if err := api.Register(); err != nil {
		return err
	}

	if clusterConfigFile != "" {
		if err := eks.LoadConfigFromFile(clusterConfigFile, cfg); err != nil {
			return err
		}
		meta = cfg.Metadata

		if meta.Name == "" {
			return fmt.Errorf("metadata.name must be set")
		}

		if meta.Region == "" {
			return fmt.Errorf("metadata.region must be set")
		}

		p.Region = meta.Region

		// Limit nodegroups to set specified on command line via globs
		if err := filterNodeGroups(cfg); err != nil {
			return err
		}

		incompatibleFlags := []string{
			"name",
			"cluster",
			"version",
			"region",
			"nodes",
			"nodes-min",
			"nodes-max",
			"node-type",
			"node-volume-size",
			"node-volume-type",
			"max-pods-per-node",
			"node-ami",
			"node-ami-family",
			"ssh-access",
			"ssh-public-key",
			"node-private-networking",
			"node-security-groups",
			"node-labels",
			"node-zones",
			"asg-access",
			"external-dns-access",
			"full-ecr-access",
		}

		for _, f := range incompatibleFlags {
			if cmd.Flag(f).Changed {
				return fmt.Errorf("cannot use --%s when --config-file/-f is set", f)
			}
		}

		if err := checkEachNodeGroup(cfg, newNodeGroupChecker); err != nil {
			return err
		}
	} else {
		// validation and defaulting specific to when --config-file is unused

		if cfg.Metadata.Name == "" {
			return errors.New("--cluster must be set")
		}

		if nodeGroupFilter != "" {
			return errors.New("cannot use --only unless a config file is specified via --config-file/-f")
		}

		err := checkEachNodeGroup(cfg, func(_ string, ng *api.NodeGroup) error {
			if ng.AllowSSH && ng.SSHPublicKeyPath == "" {
				return fmt.Errorf("--ssh-public-key must be non-empty string")
			}

			// generate nodegroup name or use either flag or argument
			if utils.NodeGroupName(ng.Name, nameArg) == "" {
				return cmdutils.ErrNameFlagAndArg(ng.Name, nameArg)
			}
			ng.Name = utils.NodeGroupName(ng.Name, nameArg)

			return nil
		})
		if err != nil {
			return err
		}

	}

	ctl := eks.New(p, cfg)

	if !ctl.IsSupportedRegion() {
		return cmdutils.ErrUnsupportedRegion(p)
	}
	logger.Info("using region %s", meta.Region)

	if err := ctl.CheckAuth(); err != nil {
		return err
	}

	if err := ctl.GetCredentials(cfg); err != nil {
		return errors.Wrapf(err, "getting credentials for cluster %q", cfg.Metadata.Name)
	}

	if err := checkVersion(ctl, cfg.Metadata); err != nil {
		return err
	}

	if err := ctl.GetClusterVPC(cfg); err != nil {
		return errors.Wrapf(err, "getting VPC configuration for cluster %q", cfg.Metadata.Name)
	}

	err := checkEachNodeGroup(cfg, func(_ string, ng *api.NodeGroup) error {
		// resolve AMI
		if err := ctl.EnsureAMI(meta.Version, ng); err != nil {
			return err
		}
		logger.Info("nodegroup %q will use %q [%s/%s]", ng.Name, ng.AMI, ng.AMIFamily, cfg.Metadata.Version)

		if err := ctl.SetNodeLabels(ng, meta); err != nil {
			return err
		}

		// load or use SSH key - name includes cluster name and the
		// fingerprint, so if unique keys provided, each will get
		// loaded and used as intended and there is no need to have
		// nodegroup name in the key name
		if err := ctl.LoadSSHPublicKey(meta.Name, ng); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	if err := printer.LogObj(logger.Debug, "cfg.json = \\\n%s\n", cfg); err != nil {
		return err
	}

	stackManager := ctl.NewStackManager(cfg)

	if err := ctl.ValidateClusterForCompatibility(cfg, stackManager); err != nil {
		return errors.Wrap(err, "cluster compatibility check failed")
	}

	{
		logger.Info("will create a CloudFormation stack for each of %d nodegroups in cluster %q", len(cfg.NodeGroups), cfg.Metadata.Name)
		errs := stackManager.CreateAllNodeGroups()
		if len(errs) > 0 {
			logger.Info("%d error(s) occurred and nodegroups haven't been created properly, you may wish to check CloudFormation console", len(errs))
			logger.Info("to cleanup resources, run 'eksctl delete nodegroup --region=%s --cluster=%s --name=<name>' for each of the failed nodegroup", cfg.Metadata.Region, cfg.Metadata.Name)
			for _, err := range errs {
				if err != nil {
					logger.Critical("%s\n", err.Error())
				}
			}
			return fmt.Errorf("failed to create nodegroups for cluster %q", cfg.Metadata.Name)
		}
	}

	{ // post-creation action
		clientSet, err := ctl.NewStdClientSet(cfg)
		if err != nil {
			return err
		}

		err = checkEachNodeGroup(cfg, func(_ string, ng *api.NodeGroup) error {
			if updateAuthConfigMap {
				// authorise nodes to join
				if err = authconfigmap.AddNodeGroup(clientSet, ng); err != nil {
					return err
				}

				// wait for nodes to join
				if err = ctl.WaitForNodes(clientSet, ng); err != nil {
					return err
				}
			}

			// if GPU instance type, give instructions
			if utils.IsGPUInstanceType(ng.InstanceType) {
				logger.Info("as you are using a GPU optimized instance type you will need to install NVIDIA Kubernetes device plugin.")
				logger.Info("\t see the following page for instructions: https://github.com/NVIDIA/k8s-device-plugin")
			}

			return nil
		})
		if err != nil {
			return err
		}
		logger.Success("created nodegroups in cluster %q", cfg.Metadata.Name)
	}

	if err := ctl.ValidateExistingNodeGroupsForCompatibility(cfg, stackManager); err != nil {
		logger.Critical("failed checking nodegroups", err.Error())
	}

	return nil
}
