package org.infinispan.cr;

import java.util.List;

import org.infinispan.cr.status.Condition;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;

import lombok.Getter;
import lombok.Setter;

@Getter
@Setter
@JsonIgnoreProperties(ignoreUnknown = true)
public class Status {
   private List<Condition> conditions;
}
