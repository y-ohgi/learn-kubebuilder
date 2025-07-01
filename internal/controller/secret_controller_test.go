/*
Copyright 2025.

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

package controller

import (
	"math/rand"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Secret Controller", func() {
	var (
		reconciler *SecretReconciler
		namespace  string
	)

	BeforeEach(func() {
		namespace = "test-" + randString(8)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		reconciler = &SecretReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
	})

	AfterEach(func() {
		// Clean up all resources in the namespace
		secretList := &corev1.SecretList{}
		k8sClient.List(ctx, secretList, client.InNamespace(namespace))
		for _, secret := range secretList.Items {
			k8sClient.Delete(ctx, &secret)
		}

		podList := &corev1.PodList{}
		k8sClient.List(ctx, podList, client.InNamespace(namespace))
		for _, pod := range podList.Items {
			k8sClient.Delete(ctx, &pod)
		}

		deploymentList := &appsv1.DeploymentList{}
		k8sClient.List(ctx, deploymentList, client.InNamespace(namespace))
		for _, deployment := range deploymentList.Items {
			k8sClient.Delete(ctx, &deployment)
		}

		saList := &corev1.ServiceAccountList{}
		k8sClient.List(ctx, saList, client.InNamespace(namespace))
		for _, sa := range saList.Items {
			if sa.Name != "default" {
				k8sClient.Delete(ctx, &sa)
			}
		}
	})

	Context("When checking unused secrets", func() {
		It("should detect unused secret", func() {
			secret := createTestSecret(namespace, "unused-secret")
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			Eventually(func() bool {
				unused, err := reconciler.isSecretUnused(ctx, secret)
				Expect(err).NotTo(HaveOccurred())
				return unused
			}).Should(BeTrue())
		})

		It("should not detect secret used by pod environment variable", func() {
			secret := createTestSecret(namespace, "used-secret")
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			pod := createTestPodWithSecretEnv(namespace, "test-pod", secret.Name)
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			Eventually(func() bool {
				unused, err := reconciler.isSecretUnused(ctx, secret)
				Expect(err).NotTo(HaveOccurred())
				return unused
			}).Should(BeFalse())
		})

		It("should not detect secret used by pod volume", func() {
			secret := createTestSecret(namespace, "volume-secret")
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			pod := createTestPodWithSecretVolume(namespace, "test-pod", secret.Name)
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			Eventually(func() bool {
				unused, err := reconciler.isSecretUnused(ctx, secret)
				Expect(err).NotTo(HaveOccurred())
				return unused
			}).Should(BeFalse())
		})

		It("should not detect secret used by deployment", func() {
			secret := createTestSecret(namespace, "deployment-secret")
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			deployment := createTestDeploymentWithSecret(namespace, "test-deployment", secret.Name)
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			Eventually(func() bool {
				unused, err := reconciler.isSecretUnused(ctx, secret)
				Expect(err).NotTo(HaveOccurred())
				return unused
			}).Should(BeFalse())
		})

		It("should not detect secret used by service account", func() {
			secret := createTestSecret(namespace, "sa-secret")
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			sa := createTestServiceAccountWithSecret(namespace, "test-sa", secret.Name)
			Expect(k8sClient.Create(ctx, sa)).To(Succeed())

			Eventually(func() bool {
				unused, err := reconciler.isSecretUnused(ctx, secret)
				Expect(err).NotTo(HaveOccurred())
				return unused
			}).Should(BeFalse())
		})

		It("should exclude service account token secrets", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sa-token-secret",
					Namespace: namespace,
					Annotations: map[string]string{
						"kubernetes.io/service-account.name": "default",
					},
				},
				Type: corev1.SecretTypeServiceAccountToken,
				Data: map[string][]byte{
					"token": []byte("fake-token"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			unused, err := reconciler.isSecretUnused(ctx, secret)
			Expect(err).NotTo(HaveOccurred())
			Expect(unused).To(BeFalse())
		})
	})

	Context("When reconciling", func() {
		It("should successfully reconcile unused secret", func() {
			secret := createTestSecret(namespace, "test-secret")
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      secret.Name,
					Namespace: secret.Namespace,
				},
			}

			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))
		})
	})
})

// Helper functions
func createTestSecret(namespace, name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"key1": []byte("value1"),
			"key2": []byte("value2"),
		},
	}
}

func createTestPodWithSecretEnv(namespace, name, secretName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "busybox",
					Env: []corev1.EnvVar{
						{
							Name: "SECRET_KEY",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: secretName,
									},
									Key: "key1",
								},
							},
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
}

func createTestPodWithSecretVolume(namespace, name, secretName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "busybox",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "secret-volume",
							MountPath: "/etc/secret",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "secret-volume",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: secretName,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
}

func createTestDeploymentWithSecret(namespace, name, secretName string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "busybox",
							Env: []corev1.EnvVar{
								{
									Name: "SECRET_KEY",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: secretName,
											},
											Key: "key1",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func createTestServiceAccountWithSecret(namespace, name, secretName string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Secrets: []corev1.ObjectReference{
			{
				Name: secretName,
			},
		},
	}
}

func int32Ptr(i int32) *int32 {
	return &i
}

func randString(n int) string {
	rand.Seed(time.Now().UnixNano())
	letters := []rune("abcdefghijklmnopqrstuvwxyz0123456789")
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
