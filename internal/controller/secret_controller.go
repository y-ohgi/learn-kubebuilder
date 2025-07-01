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
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// SecretReconciler reconciles a Secret object
type SecretReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=secrets/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Secret object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *SecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Reconciling Secret", "name", req.Name, "namespace", req.Namespace)

	secret := &corev1.Secret{}
	err := r.Get(ctx, req.NamespacedName, secret)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Info("Secret not found, might be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Secret")
		return ctrl.Result{}, err
	}

	unused, err := r.isSecretUnused(ctx, secret)
	if err != nil {
		log.Error(err, "Failed to check if secret is unused")
		return ctrl.Result{}, err
	}

	if unused {
		log.Info("UNUSED SECRET DETECTED",
			"secret", secret.Name,
			"namespace", secret.Namespace,
			"type", secret.Type,
			"created", secret.CreationTimestamp.String())
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		Named("secret").
		Complete(r)
}

func (r *SecretReconciler) isSecretUnused(ctx context.Context, secret *corev1.Secret) (bool, error) {
	if secret.Type == corev1.SecretTypeServiceAccountToken {
		return false, nil
	}

	if secret.Type == "kubernetes.io/service-account-token" {
		return false, nil
	}

	secretName := secret.Name
	secretNamespace := secret.Namespace

	pods := &corev1.PodList{}
	err := r.List(ctx, pods, client.InNamespace(secretNamespace))
	if err != nil {
		return false, fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range pods.Items {
		if r.podUsesSecret(&pod, secretName) {
			return false, nil
		}
	}

	deployments := &appsv1.DeploymentList{}
	err = r.List(ctx, deployments, client.InNamespace(secretNamespace))
	if err != nil {
		return false, fmt.Errorf("failed to list deployments: %w", err)
	}

	for _, deployment := range deployments.Items {
		if r.deploymentUsesSecret(&deployment, secretName) {
			return false, nil
		}
	}

	serviceAccounts := &corev1.ServiceAccountList{}
	err = r.List(ctx, serviceAccounts, client.InNamespace(secretNamespace))
	if err != nil {
		return false, fmt.Errorf("failed to list service accounts: %w", err)
	}

	for _, sa := range serviceAccounts.Items {
		if r.serviceAccountUsesSecret(&sa, secretName) {
			return false, nil
		}
	}

	return true, nil
}

func (r *SecretReconciler) podUsesSecret(pod *corev1.Pod, secretName string) bool {
	for _, volume := range pod.Spec.Volumes {
		if volume.Secret != nil && volume.Secret.SecretName == secretName {
			return true
		}
	}

	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil && env.ValueFrom.SecretKeyRef.Name == secretName {
				return true
			}
		}
		for _, envFrom := range container.EnvFrom {
			if envFrom.SecretRef != nil && envFrom.SecretRef.Name == secretName {
				return true
			}
		}
	}

	for _, container := range pod.Spec.InitContainers {
		for _, env := range container.Env {
			if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil && env.ValueFrom.SecretKeyRef.Name == secretName {
				return true
			}
		}
		for _, envFrom := range container.EnvFrom {
			if envFrom.SecretRef != nil && envFrom.SecretRef.Name == secretName {
				return true
			}
		}
	}

	return false
}

func (r *SecretReconciler) deploymentUsesSecret(deployment *appsv1.Deployment, secretName string) bool {
	return r.podUsesSecret(&corev1.Pod{Spec: deployment.Spec.Template.Spec}, secretName)
}

func (r *SecretReconciler) serviceAccountUsesSecret(sa *corev1.ServiceAccount, secretName string) bool {
	for _, secret := range sa.Secrets {
		if secret.Name == secretName {
			return true
		}
	}

	for _, pullSecret := range sa.ImagePullSecrets {
		if pullSecret.Name == secretName {
			return true
		}
	}

	return false
}
