package io.astrasync.control.compiler;

import io.astrasync.compiler.v1.CompilerDeliveryGuarantee;
import io.astrasync.compiler.v1.CompilerExecutionMode;
import io.astrasync.compiler.v1.CompilerTransform;
import io.astrasync.compiler.v1.CompilerValidationIssueCode;
import io.astrasync.compiler.v1.CompilerValidationServiceGrpc;
import io.astrasync.compiler.v1.EffectiveConnectorConfig;
import io.astrasync.compiler.v1.GetInventoryRequest;
import io.astrasync.compiler.v1.GetInventoryResponse;
import io.astrasync.compiler.v1.ValidateRequest;
import io.astrasync.compiler.v1.ValidateResponse;
import io.astrasync.connector.api.ConnectorDescriptor;
import io.astrasync.connector.api.ConnectorInventories;
import io.astrasync.connector.api.ConnectorRole;
import io.astrasync.control.v1.ConnectorInventory;
import io.astrasync.engine.jobspec.ConnectorSpec;
import io.astrasync.engine.jobspec.DeliveryGuarantee;
import io.astrasync.engine.jobspec.DeliverySpec;
import io.astrasync.engine.jobspec.JobConfiguration;
import io.astrasync.engine.jobspec.JobMetadata;
import io.astrasync.engine.jobspec.JobSpec;
import io.astrasync.engine.jobspec.RuntimeSpec;
import io.astrasync.engine.jobspec.TransformSpec;
import io.astrasync.engine.plan.CompilationErrorCode;
import io.astrasync.engine.plan.CompiledJobPlan;
import io.astrasync.engine.plan.ConnectorRegistry;
import io.astrasync.engine.plan.ExecutionMode;
import io.astrasync.engine.plan.JobCompilationException;
import io.astrasync.engine.plan.JobCompiler;
import io.grpc.Context;
import io.grpc.Status;
import io.grpc.stub.StreamObserver;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.concurrent.Semaphore;

public final class CompilerValidationGrpcService
        extends CompilerValidationServiceGrpc.CompilerValidationServiceImplBase {
    static final int MAX_TRANSFORMS = 128;
    private static final CompilerValidationIssueCode ISSUE_CAPABILITY_MISSING =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_CAPABILITY_MISSING;
    private static final CompilerValidationIssueCode ISSUE_CONNECTOR_NOT_FOUND =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_CONNECTOR_NOT_FOUND;
    private static final CompilerValidationIssueCode ISSUE_DELIVERY_UNSUPPORTED =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_DELIVERY_UNSUPPORTED;
    private static final CompilerValidationIssueCode ISSUE_ROLE_UNSUPPORTED =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_ROLE_UNSUPPORTED;
    private static final CompilerValidationIssueCode ISSUE_STRUCTURE =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_STRUCTURE_INVALID;
    private static final CompilerValidationIssueCode ISSUE_TRANSFORM_UNSUPPORTED =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_TRANSFORM_UNSUPPORTED;
    private static final CompilerValidationIssueCode ISSUE_REVISION_CHANGED =
            CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_VALIDATION_REVISION_CHANGED;

    private final ConnectorRegistry registry;
    private final JobCompiler compiler;
    private final ConnectorInventory inventory;
    private final String executionProfile;
    private final Semaphore concurrency;

    public CompilerValidationGrpcService(
            ConnectorRegistry registry,
            String jobSpecSchemaRevision,
            String compilerBuild,
            String executionProfile,
            int maximumConcurrency) {
        this.registry = Objects.requireNonNull(registry, "registry must not be null");
        this.compiler = new JobCompiler(registry);
        this.executionProfile = requireText(executionProfile, "executionProfile");
        if (maximumConcurrency <= 0 || maximumConcurrency > 1_024) {
            throw new IllegalArgumentException("maximumConcurrency must be between 1 and 1024");
        }
        this.concurrency = new Semaphore(maximumConcurrency);
        this.inventory = ConnectorInventories.create(
                registry.descriptors(),
                requireText(jobSpecSchemaRevision, "jobSpecSchemaRevision"),
                requireText(compilerBuild, "compilerBuild"),
                executionProfile);
    }

    @Override
    public void validate(ValidateRequest request, StreamObserver<ValidateResponse> responseObserver) {
        if (!concurrency.tryAcquire()) {
            responseObserver.onError(Status.RESOURCE_EXHAUSTED
                    .withDescription("compiler validation concurrency limit reached")
                    .asRuntimeException());
            return;
        }
        try {
            if (Context.current().isCancelled()) {
                responseObserver.onError(
                        Status.CANCELLED.withDescription("validation cancelled").asRuntimeException());
                return;
            }
            responseObserver.onNext(validateRequest(request));
            responseObserver.onCompleted();
        } catch (RuntimeException exception) {
            responseObserver.onError(Status.INTERNAL
                    .withDescription("compiler validation failed")
                    .asRuntimeException());
        } finally {
            concurrency.release();
        }
    }

    @Override
    public void getInventory(GetInventoryRequest request, StreamObserver<GetInventoryResponse> responseObserver) {
        if (request == null || !executionProfile.equals(request.getExecutionProfile())) {
            responseObserver.onError(Status.FAILED_PRECONDITION
                    .withDescription("execution profile does not match this compiler")
                    .asRuntimeException());
            return;
        }
        responseObserver.onNext(
                GetInventoryResponse.newBuilder().setInventory(inventory).build());
        responseObserver.onCompleted();
    }

    ValidateResponse validateRequest(ValidateRequest request) {
        ValidationIssues issues = new ValidationIssues();
        ValidateResponse.Builder result = ValidateResponse.newBuilder()
                .setCompilerRevision(inventory.getCompilerRevision())
                .setInventoryRevision(inventory.getInventoryRevision());
        if (request == null) {
            issues.add(ISSUE_STRUCTURE, "spec", "validation request is required");
            return finish(result, issues);
        }
        if (!executionProfile.equals(request.getExecutionProfile())) {
            issues.add(ISSUE_REVISION_CHANGED, "spec", "execution profile changed; refresh and validate again");
        }
        if (!request.getExpectedCompilerRevision().isEmpty()
                && !inventory.getCompilerRevision().equals(request.getExpectedCompilerRevision())) {
            issues.add(ISSUE_REVISION_CHANGED, "spec", "compiler revision changed; refresh and validate again");
        }
        if (!validName(request.getName())) {
            issues.add(ISSUE_STRUCTURE, "name", "job name must be a lowercase DNS label");
        }
        if (!request.hasSource() || !request.hasSink()) {
            issues.add(ISSUE_STRUCTURE, "spec", "source and sink are required");
            return finish(result, issues);
        }
        if (request.getTransformsCount() > MAX_TRANSFORMS) {
            issues.add(ISSUE_STRUCTURE, "spec.transforms", "transform count exceeds the supported limit");
        }
        if (request.getMaxBatchRecords() <= 0) {
            issues.add(ISSUE_STRUCTURE, "spec.runtime.max_batch_records", "max batch records must be positive");
        }

        Map<String, String> sourceOptions =
                validateConnector(request.getSource(), ConnectorRole.SOURCE, "spec.source", issues);
        Map<String, String> sinkOptions = validateConnector(request.getSink(), ConnectorRole.SINK, "spec.sink", issues);
        DeliveryGuarantee delivery = delivery(request.getDeliveryGuarantee(), issues);
        List<TransformSpec> transforms = transforms(request.getTransformsList(), issues);
        if (!issues.isEmpty()) {
            return finish(result, issues);
        }

        try {
            JobSpec jobSpec = new JobSpec(
                    JobSpec.API_VERSION,
                    JobSpec.KIND,
                    new JobMetadata(request.getName()),
                    new JobConfiguration(
                            new ConnectorSpec(request.getSource().getConnector(), sourceOptions),
                            transforms,
                            new ConnectorSpec(request.getSink().getConnector(), sinkOptions),
                            new DeliverySpec(delivery),
                            new RuntimeSpec(request.getMaxBatchRecords())));
            CompiledJobPlan plan = compiler.compileCheckpointed(jobSpec);
            result.setExecutionMode(
                    plan.executionMode() == ExecutionMode.CDC
                            ? CompilerExecutionMode.COMPILER_EXECUTION_MODE_CDC
                            : CompilerExecutionMode.COMPILER_EXECUTION_MODE_BATCH);
        } catch (JobCompilationException exception) {
            issues.add(
                    issueCode(exception.code()),
                    compilationPath(exception.code()),
                    safeCompilationMessage(exception.code()));
        } catch (IllegalArgumentException exception) {
            issues.add(ISSUE_STRUCTURE, "spec", "job specification is structurally invalid");
        }
        return finish(result, issues);
    }

    private Map<String, String> validateConnector(
            EffectiveConnectorConfig config, ConnectorRole role, String path, ValidationIssues issues) {
        if (config.getConnector().isEmpty() || config.getConnector().length() > 128) {
            issues.add(ISSUE_STRUCTURE, path + ".connector", "connector name is invalid");
            return Map.of();
        }
        ConnectorDescriptor descriptor =
                registry.findDescriptor(config.getConnector()).orElse(null);
        if (descriptor == null) {
            issues.add(ISSUE_CONNECTOR_NOT_FOUND, path + ".connector", "connector is not available in this deployment");
            return Map.of();
        }
        return DescriptorOptionValidator.validate(config, descriptor, role, path, issues);
    }

    private static DeliveryGuarantee delivery(CompilerDeliveryGuarantee source, ValidationIssues issues) {
        return switch (source) {
            case COMPILER_DELIVERY_GUARANTEE_EXACTLY_ONCE -> DeliveryGuarantee.EXACTLY_ONCE;
            case COMPILER_DELIVERY_GUARANTEE_AT_LEAST_ONCE -> DeliveryGuarantee.AT_LEAST_ONCE;
            case COMPILER_DELIVERY_GUARANTEE_AT_MOST_ONCE -> DeliveryGuarantee.AT_MOST_ONCE;
            default -> {
                issues.add(ISSUE_STRUCTURE, "spec.delivery.guarantee", "delivery guarantee is required");
                yield DeliveryGuarantee.AT_MOST_ONCE;
            }
        };
    }

    private static List<TransformSpec> transforms(List<CompilerTransform> source, ValidationIssues issues) {
        List<TransformSpec> result = new ArrayList<>(Math.min(source.size(), MAX_TRANSFORMS));
        for (int index = 0; index < source.size() && index < MAX_TRANSFORMS; index++) {
            CompilerTransform transform = source.get(index);
            if (transform.getType().isBlank()
                    || transform.getType().length() > 128
                    || transform.getOptionsCount() > DescriptorOptionValidator.MAX_OPTIONS) {
                issues.add(ISSUE_STRUCTURE, "spec.transforms[" + index + "]", "transform definition is invalid");
                continue;
            }
            result.add(new TransformSpec(transform.getType(), transform.getOptionsMap()));
        }
        return List.copyOf(result);
    }

    private static ValidateResponse finish(ValidateResponse.Builder result, ValidationIssues issues) {
        return result.setValid(issues.isEmpty()).addAllIssues(issues.result()).build();
    }

    private static CompilerValidationIssueCode issueCode(CompilationErrorCode code) {
        return switch (code) {
            case CONNECTOR_NOT_FOUND -> ISSUE_CONNECTOR_NOT_FOUND;
            case ROLE_UNSUPPORTED -> ISSUE_ROLE_UNSUPPORTED;
            case CAPABILITY_MISSING -> ISSUE_CAPABILITY_MISSING;
            case TRANSFORM_UNSUPPORTED -> ISSUE_TRANSFORM_UNSUPPORTED;
            case DELIVERY_UNSUPPORTED -> ISSUE_DELIVERY_UNSUPPORTED;
        };
    }

    private static String compilationPath(CompilationErrorCode code) {
        return switch (code) {
            case CONNECTOR_NOT_FOUND, ROLE_UNSUPPORTED, CAPABILITY_MISSING -> "spec";
            case TRANSFORM_UNSUPPORTED -> "spec.transforms";
            case DELIVERY_UNSUPPORTED -> "spec.delivery.guarantee";
        };
    }

    private static String safeCompilationMessage(CompilationErrorCode code) {
        return switch (code) {
            case CONNECTOR_NOT_FOUND -> "connector is not available in this deployment";
            case ROLE_UNSUPPORTED -> "connector does not support the requested role";
            case CAPABILITY_MISSING -> "connector combination lacks a required execution capability";
            case TRANSFORM_UNSUPPORTED -> "configured transforms are not supported by this runtime";
            case DELIVERY_UNSUPPORTED -> "delivery guarantee is not supported by this connector combination";
        };
    }

    private static boolean validName(String value) {
        return value != null && value.matches("[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?");
    }

    private static String requireText(String value, String label) {
        Objects.requireNonNull(value, label + " must not be null");
        if (value.isBlank() || value.length() > 256) {
            throw new IllegalArgumentException(label + " must be between 1 and 256 characters");
        }
        return value;
    }
}
