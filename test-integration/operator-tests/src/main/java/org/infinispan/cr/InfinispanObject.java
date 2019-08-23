package org.infinispan.cr;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;

import lombok.Getter;
import lombok.Setter;

@Getter
@Setter
@JsonIgnoreProperties(ignoreUnknown = true)
public class InfinispanObject {
   private Metadata metadata;
   private Spec spec;
   private Status status;
}
