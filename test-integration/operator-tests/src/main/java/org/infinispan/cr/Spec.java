package org.infinispan.cr;

import org.infinispan.cr.spec.Security;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;

import lombok.Getter;
import lombok.Setter;

@Getter
@Setter
@JsonIgnoreProperties(ignoreUnknown = true)
public class Spec {
   private int replicas;
   private Security security;
   private String version;
}
