package io.astrasync.control.compiler;

import io.astrasync.compiler.v1.CompilerValidationIssue;
import io.astrasync.compiler.v1.CompilerValidationIssueCode;
import java.util.ArrayList;
import java.util.List;

final class ValidationIssues {
    static final int MAX_ISSUES = 32;
    private static final int MAX_MESSAGE_LENGTH = 256;

    private final List<CompilerValidationIssue> values = new ArrayList<>();
    private boolean truncated;

    void add(CompilerValidationIssueCode code, String fieldPath, String message) {
        add(code, fieldPath, message, "");
    }

    void add(CompilerValidationIssueCode code, String fieldPath, String message, String documentationKey) {
        if (values.size() >= MAX_ISSUES - 1) {
            truncated = true;
            return;
        }
        values.add(CompilerValidationIssue.newBuilder()
                .setCode(code)
                .setFieldPath(bounded(fieldPath))
                .setMessage(bounded(message))
                .setDocumentationKey(bounded(documentationKey))
                .build());
    }

    boolean isEmpty() {
        return values.isEmpty() && !truncated;
    }

    List<CompilerValidationIssue> result() {
        if (!truncated) {
            return List.copyOf(values);
        }
        List<CompilerValidationIssue> result = new ArrayList<>(values);
        result.add(CompilerValidationIssue.newBuilder()
                .setCode(CompilerValidationIssueCode.COMPILER_VALIDATION_ISSUE_CODE_ISSUES_TRUNCATED)
                .setFieldPath("spec")
                .setMessage("additional validation issues were omitted")
                .build());
        return List.copyOf(result);
    }

    private static String bounded(String value) {
        if (value == null) {
            return "";
        }
        return value.length() <= MAX_MESSAGE_LENGTH ? value : value.substring(0, MAX_MESSAGE_LENGTH);
    }
}
