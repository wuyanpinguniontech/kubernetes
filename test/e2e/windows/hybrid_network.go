/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package windows

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/kubernetes/test/e2e/framework"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"
	e2eskipper "k8s.io/kubernetes/test/e2e/framework/skipper"
	admissionapi "k8s.io/pod-security-admission/api"

	imageutils "k8s.io/kubernetes/test/utils/image"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

const (
	linuxOS   = "linux"
	windowsOS = "windows"
)

var (
	windowsBusyBoximage = imageutils.GetE2EImage(imageutils.Agnhost)
	linuxBusyBoxImage   = imageutils.GetE2EImage(imageutils.Nginx)
)

var _ = SIGDescribe("Hybrid cluster network", func() {
	f := framework.NewDefaultFramework("hybrid-network")
	f.NamespacePodSecurityEnforceLevel = admissionapi.LevelPrivileged

	ginkgo.BeforeEach(func() {
		e2eskipper.SkipUnlessNodeOSDistroIs("windows")
	})

	ginkgo.Context("for all supported CNIs", func() {

		ginkgo.It("should have stable networking for Linux and Windows pods", func(ctx context.Context) {

			linuxPod := createTestPod(f, linuxBusyBoxImage, linuxOS)
			ginkgo.By("creating a linux pod and waiting for it to be running")
			linuxPod = e2epod.NewPodClient(f).CreateSync(linuxPod)

			windowsPod := createTestPod(f, windowsBusyBoximage, windowsOS)

			windowsPod.Spec.Containers[0].Args = []string{"test-webserver"}
			ginkgo.By("creating a windows pod and waiting for it to be running")
			windowsPod = e2epod.NewPodClient(f).CreateSync(windowsPod)

			ginkgo.By("verifying pod internal connectivity to the cluster dataplane")

			ginkgo.By("checking connectivity from Linux to Windows")
			assertConsistentConnectivity(f, linuxPod.ObjectMeta.Name, linuxOS, linuxCheck(windowsPod.Status.PodIP, 80))

			ginkgo.By("checking connectivity from Windows to Linux")
			assertConsistentConnectivity(f, windowsPod.ObjectMeta.Name, windowsOS, windowsCheck(linuxPod.Status.PodIP))

		})

		ginkgo.It("should provide Internet connection for Linux containers using DNS [Feature:Networking-DNS]", func(ctx context.Context) {
			linuxPod := createTestPod(f, linuxBusyBoxImage, linuxOS)
			ginkgo.By("creating a linux pod and waiting for it to be running")
			linuxPod = e2epod.NewPodClient(f).CreateSync(linuxPod)

			ginkgo.By("verifying pod external connectivity to the internet")

			ginkgo.By("checking connectivity to 8.8.8.8 53 (google.com) from Linux")
			assertConsistentConnectivity(f, linuxPod.ObjectMeta.Name, linuxOS, linuxCheck("8.8.8.8", 53))
		})

		ginkgo.It("should provide Internet connection for Windows containers using DNS [Feature:Networking-DNS]", func(ctx context.Context) {
			windowsPod := createTestPod(f, windowsBusyBoximage, windowsOS)
			ginkgo.By("creating a windows pod and waiting for it to be running")
			windowsPod = e2epod.NewPodClient(f).CreateSync(windowsPod)

			ginkgo.By("verifying pod external connectivity to the internet")

			ginkgo.By("checking connectivity to 8.8.8.8 53 (google.com) from Windows")
			assertConsistentConnectivity(f, windowsPod.ObjectMeta.Name, windowsOS, windowsCheck("www.google.com"))
		})

	})
})

var (
	duration       = "10s"
	pollInterval   = "1s"
	timeoutSeconds = 10
)

func assertConsistentConnectivity(f *framework.Framework, podName string, os string, cmd []string) {
	connChecker := func() error {
		ginkgo.By(fmt.Sprintf("checking connectivity of %s-container in %s", os, podName))
		// TODO, we should be retrying this similar to what is done in DialFromNode, in the test/e2e/networking/networking.go tests
		stdout, stderr, err := e2epod.ExecCommandInContainerWithFullOutput(f, podName, os+"-container", cmd...)
		if err != nil {
			framework.Logf("Encountered error while running command: %v.\nStdout: %s\nStderr: %s\nErr: %v", cmd, stdout, stderr, err)
		}
		return err
	}
	gomega.Eventually(connChecker, duration, pollInterval).ShouldNot(gomega.HaveOccurred())
	gomega.Consistently(connChecker, duration, pollInterval).ShouldNot(gomega.HaveOccurred())
}

func linuxCheck(address string, port int) []string {
	nc := fmt.Sprintf("nc -vz %s %v -w %v", address, port, timeoutSeconds)
	cmd := []string{"/bin/sh", "-c", nc}
	return cmd
}

func windowsCheck(address string) []string {
	curl := fmt.Sprintf("curl.exe %s --connect-timeout %v --fail", address, timeoutSeconds)
	cmd := []string{"cmd", "/c", curl}
	return cmd
}

func createTestPod(f *framework.Framework, image string, os string) *v1.Pod {
	containerName := fmt.Sprintf("%s-container", os)
	podName := "pod-" + string(uuid.NewUUID())
	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  containerName,
					Image: image,
					Ports: []v1.ContainerPort{{ContainerPort: 80}},
				},
			},
			NodeSelector: map[string]string{
				"kubernetes.io/os": os,
			},
		},
	}
	if os == linuxOS {
		pod.Spec.Tolerations = []v1.Toleration{
			{
				Operator: v1.TolerationOpExists,
				Effect:   v1.TaintEffectNoSchedule,
			},
		}
	}
	return pod
}
