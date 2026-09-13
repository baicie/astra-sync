package io.astrasync.engine.observability;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;
import org.slf4j.MDC;

class DataPlaneLogContextTest {
    @Test
    void setsAndRestoresStructuredFields() {
        MDC.put("tenant_id", "outer-tenant");
        try {
            try (DataPlaneLogContext ignored =
                    DataPlaneLogContext.open("inner-tenant", "job-1", 4L, "read", "success")) {
                assertThat(MDC.get("tenant_id")).isEqualTo("inner-tenant");
                assertThat(MDC.get("job_id")).isEqualTo("job-1");
                assertThat(MDC.get("epoch")).isEqualTo("4");
                assertThat(MDC.get("stage")).isEqualTo("read");
                assertThat(MDC.get("outcome")).isEqualTo("success");
            }

            assertThat(MDC.get("tenant_id")).isEqualTo("outer-tenant");
            assertThat(MDC.get("job_id")).isNull();
            assertThat(MDC.get("epoch")).isNull();
            assertThat(MDC.get("stage")).isNull();
            assertThat(MDC.get("outcome")).isNull();
        } finally {
            MDC.clear();
        }
    }

    @Test
    void nestedContextRestoresOuterStage() {
        try (DataPlaneLogContext outer = DataPlaneLogContext.open("tenant-1", "job-1", 1L, "read", null)) {
            try (DataPlaneLogContext ignored = DataPlaneLogContext.open("tenant-1", "job-1", 1L, "write", "success")) {
                assertThat(MDC.get("stage")).isEqualTo("write");
                assertThat(MDC.get("outcome")).isEqualTo("success");
            }

            assertThat(MDC.get("stage")).isEqualTo("read");
            assertThat(MDC.get("outcome")).isNull();
        } finally {
            MDC.clear();
        }
    }

    @Test
    void ignoresBlankFieldsAndNonPositiveEpoch() {
        try (DataPlaneLogContext ignored = DataPlaneLogContext.open(" ", null, 0L, "", null)) {
            assertThat(MDC.get("tenant_id")).isNull();
            assertThat(MDC.get("job_id")).isNull();
            assertThat(MDC.get("epoch")).isNull();
            assertThat(MDC.get("stage")).isNull();
        } finally {
            MDC.clear();
        }
    }
}
